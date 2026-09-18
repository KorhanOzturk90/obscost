package timeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Store keeps snapshots and the changes derived from them. It is kept to
// the two operations a capture needs, so a warehouse sink (issue #32) can
// implement it without also implementing file-style browsing.
type Store interface {
	// Latest returns tenant's most recent snapshot, or nil and no error if
	// the store has none for it.
	Latest(ctx context.Context, tenant string) (*Snapshot, error)
	// Put stores snap together with the changes that separate it from the
	// snapshot Latest returned before the call (none for a tenant's first
	// snapshot).
	Put(ctx context.Context, snap Snapshot, changes []Change) error
}

// Record diffs snap against the store's latest snapshot for the same
// tenant, stores both, and returns the changes. The first snapshot of a
// tenant has no predecessor and yields no changes: it is the baseline, not
// a batch of additions.
func Record(ctx context.Context, store Store, snap Snapshot) ([]Change, error) {
	if err := snap.Validate(); err != nil {
		return nil, err
	}
	prev, err := store.Latest(ctx, snap.Tenant)
	if err != nil {
		return nil, err
	}
	var changes []Change
	if prev != nil {
		if changes, err = Diff(*prev, snap); err != nil {
			return nil, err
		}
	}
	if err := store.Put(ctx, snap, changes); err != nil {
		return nil, err
	}
	return changes, nil
}

// DirStore is a Store over a local directory:
//
//	<root>/<tenant>/<observed_at>.json   one Snapshot per file
//	<root>/<tenant>/changes.ndjson      one Change per line, appended by Put
//
// observed_at is UTC in snapshotTimeLayout, so file names sort in time
// order. The snapshot files are the source of truth. changes.ndjson is the
// same changes kept as an append-only event log for whatever reads the
// timeline next. If a capture dies between writing the snapshot and
// appending its changes, `promcost timeline diff` over the directory still
// derives them from the snapshots.
type DirStore struct {
	Root string
}

const (
	snapshotTimeLayout = "20060102T150405.000000000Z"
	changesFile        = "changes.ndjson"
)

// SnapshotPath is where Put writes snap.
func (d DirStore) SnapshotPath(snap Snapshot) string {
	return filepath.Join(d.Root, snap.Tenant, snap.ObservedAt.UTC().Format(snapshotTimeLayout)+".json")
}

func (d DirStore) Latest(_ context.Context, tenant string) (*Snapshot, error) {
	if err := ValidateTenant(tenant); err != nil {
		return nil, err
	}
	names, err := d.snapshotFiles(tenant)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, nil
	}
	snap, err := ReadSnapshotFile(names[len(names)-1])
	if err != nil {
		return nil, err
	}
	if snap.Tenant != tenant {
		return nil, fmt.Errorf("%s: snapshot is for tenant %q, not %q", names[len(names)-1], snap.Tenant, tenant)
	}
	return &snap, nil
}

func (d DirStore) Put(_ context.Context, snap Snapshot, changes []Change) error {
	if err := snap.Validate(); err != nil {
		return err
	}
	dir := filepath.Join(d.Root, snap.Tenant)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	if err := writeNewFile(d.SnapshotPath(snap), append(data, '\n')); err != nil {
		return err
	}

	if len(changes) == 0 {
		return nil
	}
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	for _, c := range changes {
		if err := enc.Encode(c); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(filepath.Join(dir, changesFile), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(buf.String()); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// snapshotFiles lists tenant's snapshot files, oldest first.
func (d DirStore) snapshotFiles(tenant string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(d.Root, tenant))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, filepath.Join(d.Root, tenant, e.Name()))
		}
	}
	slices.Sort(names)
	return names, nil
}

// writeNewFile writes data to a temporary file and hard-links it into
// place, so a reader never sees a half-written snapshot and an existing
// file is never replaced (os.Link fails if path exists).
func writeNewFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".snapshot-*.tmp")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Link(tmp.Name(), path)
}

// ReadSnapshotFile reads and validates one snapshot file. Unknown fields
// are rejected, so a file that isn't a snapshot fails loudly rather than
// decoding as an empty one.
func ReadSnapshotFile(path string) (Snapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		return Snapshot{}, err
	}
	defer func() { _ = f.Close() }()

	var snap Snapshot
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&snap); err != nil {
		return Snapshot{}, fmt.Errorf("%s: %w", path, err)
	}
	if err := snap.Validate(); err != nil {
		return Snapshot{}, fmt.Errorf("%s: %w", path, err)
	}
	return snap, nil
}

// ReadSnapshotDir reads every *.json file under root, at any depth, as a
// snapshot. It depends only on file contents, not on DirStore's layout, so
// snapshots gathered from several places can be dropped in one directory.
// Files and directories whose names start with "." are skipped.
func ReadSnapshotDir(root string) ([]Snapshot, error) {
	var snaps []Snapshot
	err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != root && strings.HasPrefix(e.Name(), ".") {
			if e.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".json") {
			return nil
		}
		snap, err := ReadSnapshotFile(path)
		if err != nil {
			return err
		}
		snaps = append(snaps, snap)
		return nil
	})
	return snaps, err
}
