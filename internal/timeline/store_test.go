package timeline

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDirStore_RecordBaselineThenChanges(t *testing.T) {
	ctx := context.Background()
	store := DirStore{Root: t.TempDir()}

	if latest, err := store.Latest(ctx, "team-a"); err != nil || latest != nil {
		t.Fatalf("Latest on empty store = %v, %v; want nil, nil", latest, err)
	}

	first := snapAt(0, group("a.yaml", "g", 60, rec("x", "up")))
	changes, err := Record(ctx, store, first)
	if err != nil {
		t.Fatalf("Record first: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("first snapshot yielded %v; a baseline has no changes", summary(changes))
	}
	if _, err := os.Stat(filepath.Join(store.Root, "team-a", changesFile)); !os.IsNotExist(err) {
		t.Errorf("changes file exists after a baseline-only capture (err=%v)", err)
	}

	latest, err := store.Latest(ctx, "team-a")
	if err != nil || latest == nil {
		t.Fatalf("Latest = %v, %v", latest, err)
	}
	if !reflect.DeepEqual(*latest, first) {
		t.Errorf("Latest round trip:\n got %+v\nwant %+v", *latest, first)
	}

	second := snapAt(5, group("a.yaml", "g", 60, rec("x", "up == 1")))
	changes, err = Record(ctx, store, second)
	if err != nil {
		t.Fatalf("Record second: %v", err)
	}
	if got := summary(changes); len(got) != 1 || got[0] != "changed team-a/a.yaml/g/x [expr]" {
		t.Errorf("second capture changes = %v", got)
	}

	third := snapAt(10)
	if _, err := Record(ctx, store, third); err != nil {
		t.Fatalf("Record third: %v", err)
	}

	// changes.ndjson holds one line per change, in capture order, each
	// decoding back to the Change that was returned.
	f, err := os.Open(filepath.Join(store.Root, "team-a", changesFile))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var logged []Change
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var c Change
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			t.Fatalf("changes.ndjson line %q: %v", sc.Text(), err)
		}
		logged = append(logged, c)
	}
	if got := summary(logged); len(got) != 2 || got[0] != "changed team-a/a.yaml/g/x [expr]" || got[1] != "removed team-a/a.yaml/g/x" {
		t.Errorf("changes.ndjson = %v", got)
	}
	if !logged[1].Window.After.Equal(second.RequestedAt) || !logged[1].Window.NotAfter.Equal(third.ObservedAt) {
		t.Errorf("logged window = %+v", logged[1].Window)
	}

	// The same timeline comes out of the directory's snapshots.
	snaps, err := ReadSnapshotDir(store.Root)
	if err != nil {
		t.Fatalf("ReadSnapshotDir: %v", err)
	}
	tl, err := Build(snaps)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(tl.Tenants) != 1 || !reflect.DeepEqual(summary(tl.Tenants[0].Changes), summary(logged)) {
		t.Errorf("Build over the directory = %+v, want the logged changes %v", tl, summary(logged))
	}
}

func TestDirStore_RefusesToOverwrite(t *testing.T) {
	ctx := context.Background()
	store := DirStore{Root: t.TempDir()}
	snap := snapAt(0)
	if err := store.Put(ctx, snap, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, snap, nil); err == nil {
		t.Error("second Put of the same snapshot: want error, got nil")
	}
	entries, _ := os.ReadDir(filepath.Join(store.Root, "team-a"))
	if len(entries) != 1 {
		t.Errorf("tenant dir has %d entries, want 1 (no leftover temp files)", len(entries))
	}
}

func TestRecord_RejectsSnapshotOlderThanLatest(t *testing.T) {
	ctx := context.Background()
	store := DirStore{Root: t.TempDir()}
	if _, err := Record(ctx, store, snapAt(10)); err != nil {
		t.Fatal(err)
	}
	if _, err := Record(ctx, store, snapAt(0)); err == nil {
		t.Error("recording a snapshot older than the latest: want error")
	}
}

func TestValidateTenant(t *testing.T) {
	for _, bad := range []string{"", ".", "..", "a/b", `a\b`, "a\x00b"} {
		if ValidateTenant(bad) == nil {
			t.Errorf("ValidateTenant(%q) = nil, want error", bad)
		}
	}
	for _, good := range []string{"team-a", "anonymous", "a.b_c*(d)!"} {
		if err := ValidateTenant(good); err != nil {
			t.Errorf("ValidateTenant(%q) = %v", good, err)
		}
	}
}

func TestReadSnapshotDir_SkipsHiddenAndRejectsForeignJSON(t *testing.T) {
	root := t.TempDir()
	store := DirStore{Root: root}
	if err := store.Put(context.Background(), snapAt(0), nil); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "x.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	snaps, err := ReadSnapshotDir(root)
	if err != nil || len(snaps) != 1 {
		t.Fatalf("ReadSnapshotDir = %d snapshots, %v; want 1, nil", len(snaps), err)
	}

	if err := os.WriteFile(filepath.Join(root, "other.json"), []byte(`{"status":"success"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSnapshotDir(root); err == nil || !strings.Contains(err.Error(), "other.json") {
		t.Errorf("ReadSnapshotDir with a non-snapshot .json file: err = %v, want one naming the file", err)
	}
}

func TestReadSnapshotFile_RejectsWrongVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	snap := snapAt(0)
	snap.Version = 99
	data, _ := json.Marshal(snap)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSnapshotFile(path); err == nil {
		t.Error("want an error for an unknown format version")
	}
}
