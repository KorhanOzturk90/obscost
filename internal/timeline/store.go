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
	"time"
)

// Store keeps snapshots and the changes derived from them. It is kept to
// the operations a capture needs, so a warehouse sink (issue #32) can
// implement it without also implementing file-style browsing.
type Store interface {
	// Latest returns tenant's most recent snapshot, with its LastSeen set
	// if MarkSeen has recorded one, or nil and no error if the store has
	// none for it.
	Latest(ctx context.Context, tenant string) (*Snapshot, error)
	// Put stores snap together with the changes that separate it from the
	// snapshot Latest returned before the call (none for a tenant's first
	// snapshot).
	Put(ctx context.Context, snap Snapshot, changes []Change) error
	// MarkSeen records that a capture found the definitions of the
	// snapshot seen.Snapshot names unchanged, replacing any earlier
	// LastSeen for that snapshot. It stores no new snapshot.
	MarkSeen(ctx context.Context, seen LastSeen) error
}

// Record diffs snap against the store's latest snapshot for the same
// tenant and returns the changes. The first snapshot of a tenant has no
// predecessor and yields no changes: it is the baseline, not a batch of
// additions.
//
// If snap's definitions are identical to the latest snapshot's, Record
// stores no new snapshot. It calls MarkSeen instead and reports unchanged,
// so the next change's window still starts at this capture. Otherwise it
// stores snap and its changes with Put.
func Record(ctx context.Context, store Store, snap Snapshot) (changes []Change, unchanged bool, err error) {
	if err := snap.Validate(); err != nil {
		return nil, false, err
	}
	prev, err := store.Latest(ctx, snap.Tenant)
	if err != nil {
		return nil, false, err
	}
	if prev != nil {
		// Diff first even when nothing changed: it also rejects a snapshot
		// that isn't strictly after the latest capture.
		if changes, err = Diff(*prev, snap); err != nil {
			return nil, false, err
		}
		same, err := sameDefinitions(*prev, snap)
		if err != nil {
			return nil, false, err
		}
		if same {
			err := store.MarkSeen(ctx, LastSeen{
				Version:     FormatVersion,
				Tenant:      snap.Tenant,
				Snapshot:    prev.ObservedAt,
				Captures:    prev.captures() + 1,
				RequestedAt: snap.RequestedAt,
				ObservedAt:  snap.ObservedAt,
			})
			return nil, err == nil, err
		}
	}
	if err := store.Put(ctx, snap, changes); err != nil {
		return nil, false, err
	}
	return changes, false, nil
}

// DirStore is a Store over a local directory:
//
//	<root>/<tenant>/<observed_at>.json        one Snapshot per file, never rewritten
//	<root>/<tenant>/<observed_at>.seen.json   that snapshot's LastSeen, replaced by MarkSeen
//	<root>/<tenant>/changes.ndjson           one Change per line, appended by Put
//
// observed_at is UTC in snapshotTimeLayout, so file names sort in time
// order. A rule set that never changes therefore costs one snapshot file
// and one small .seen.json, however often it is captured. The snapshot and
// .seen.json files are the source of truth. changes.ndjson is the
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
	lastSeenSuffix     = ".seen.json"
)

// SnapshotPath is where Put writes snap.
func (d DirStore) SnapshotPath(snap Snapshot) string {
	return d.snapshotPath(snap.Tenant, snap.ObservedAt)
}

// LastSeenPath is where MarkSeen writes the LastSeen of tenant's snapshot
// observed at snapshotObservedAt.
func (d DirStore) LastSeenPath(tenant string, snapshotObservedAt time.Time) string {
	return strings.TrimSuffix(d.snapshotPath(tenant, snapshotObservedAt), ".json") + lastSeenSuffix
}

func (d DirStore) snapshotPath(tenant string, observedAt time.Time) string {
	return filepath.Join(d.Root, tenant, observedAt.UTC().Format(snapshotTimeLayout)+".json")
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
	seenPath := d.LastSeenPath(tenant, snap.ObservedAt)
	seen, err := readLastSeenFile(seenPath)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := snap.attach(seen); err != nil {
			return nil, fmt.Errorf("%s: %w", seenPath, err)
		}
	}
	return &snap, nil
}

// MarkSeen replaces the .seen.json of the snapshot seen names. The snapshot
// file must exist. The write is a rename over the old file, so a reader
// sees either the old LastSeen or the new one.
func (d DirStore) MarkSeen(_ context.Context, seen LastSeen) error {
	if err := seen.Validate(); err != nil {
		return err
	}
	if _, err := os.Stat(d.snapshotPath(seen.Tenant, seen.Snapshot)); err != nil {
		return fmt.Errorf("no snapshot for this last-seen record: %w", err)
	}
	data, err := json.MarshalIndent(seen, "", "  ")
	if err != nil {
		return err
	}
	return replaceFile(d.LastSeenPath(seen.Tenant, seen.Snapshot), append(data, '\n'))
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
		if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".json") && !strings.HasSuffix(e.Name(), lastSeenSuffix) {
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

// replaceFile writes data to a temporary file and renames it over path, so
// a reader sees either the old contents or the new, never a partial file.
func replaceFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".seen-*.tmp")
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
	return os.Rename(tmp.Name(), path)
}

// readLastSeenFile reads and validates one .seen.json file.
func readLastSeenFile(path string) (LastSeen, error) {
	f, err := os.Open(path)
	if err != nil {
		return LastSeen{}, err
	}
	defer func() { _ = f.Close() }()

	var seen LastSeen
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&seen); err != nil {
		return LastSeen{}, fmt.Errorf("%s: %w", path, err)
	}
	if err := seen.Validate(); err != nil {
		return LastSeen{}, fmt.Errorf("%s: %w", path, err)
	}
	return seen, nil
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

// ReadSnapshotDir reads every *.json file under root, at any depth, with
// ReadSnapshotFiles. It depends only on file contents, not on DirStore's
// layout, so snapshots gathered from several places can be dropped in one
// directory. Files and directories whose names start with "." are skipped.
func ReadSnapshotDir(root string) ([]Snapshot, error) {
	var paths []string
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
		if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".json") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ReadSnapshotFiles(paths)
}

// ReadSnapshotFiles reads each path as a snapshot, except *.seen.json files,
// which it reads as LastSeen records and attaches to the snapshot each one
// names (the latest wins if several name the same one). It matches them by
// content, tenant and the snapshot's observed_at, not by file name. A
// LastSeen whose snapshot isn't among paths is an error. Snapshots read
// without their LastSeen are still diffed correctly, only with wider
// windows: the next change's window then starts at the snapshot's own
// capture instead of the last one that saw it unchanged.
func ReadSnapshotFiles(paths []string) ([]Snapshot, error) {
	var (
		snaps     []Snapshot
		seens     []LastSeen
		seenPaths []string
	)
	for _, path := range paths {
		if strings.HasSuffix(path, lastSeenSuffix) {
			seen, err := readLastSeenFile(path)
			if err != nil {
				return nil, err
			}
			seens, seenPaths = append(seens, seen), append(seenPaths, path)
			continue
		}
		snap, err := ReadSnapshotFile(path)
		if err != nil {
			return nil, err
		}
		snaps = append(snaps, snap)
	}

	type key struct {
		tenant string
		at     int64
	}
	index := make(map[key]int, len(snaps))
	for i, s := range snaps {
		index[key{s.Tenant, s.ObservedAt.UnixNano()}] = i
	}
	for i, seen := range seens {
		j, ok := index[key{seen.Tenant, seen.Snapshot.UnixNano()}]
		if !ok {
			return nil, fmt.Errorf("%s: no snapshot of tenant %s observed at %s", seenPaths[i], seen.Tenant, seen.Snapshot.Format(time.RFC3339Nano))
		}
		if cur := snaps[j].LastSeen; cur != nil && !seen.ObservedAt.After(cur.ObservedAt) {
			continue
		}
		if err := snaps[j].attach(seen); err != nil {
			return nil, fmt.Errorf("%s: %w", seenPaths[i], err)
		}
	}
	return snaps, nil
}
