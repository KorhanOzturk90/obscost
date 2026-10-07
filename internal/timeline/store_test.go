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
	changes, _, err := Record(ctx, store, first)
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
	changes, _, err = Record(ctx, store, second)
	if err != nil {
		t.Fatalf("Record second: %v", err)
	}
	if got := summary(changes); len(got) != 1 || got[0] != "changed team-a/a.yaml/g/x [expr]" {
		t.Errorf("second capture changes = %v", got)
	}

	third := snapAt(10)
	if _, _, err := Record(ctx, store, third); err != nil {
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
	if _, _, err := Record(ctx, store, snapAt(10)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Record(ctx, store, snapAt(0)); err == nil {
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

// tenantFiles lists the tenant directory's snapshot and last-seen files.
func tenantFiles(t *testing.T, store DirStore, tenant string) (snapshots, seen []string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(store.Root, tenant))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		switch {
		case strings.HasSuffix(e.Name(), lastSeenSuffix):
			seen = append(seen, e.Name())
		case strings.HasSuffix(e.Name(), ".json"):
			snapshots = append(snapshots, e.Name())
		}
	}
	return snapshots, seen
}

func TestRecord_IdenticalCaptureStoresNoSnapshot(t *testing.T) {
	ctx := context.Background()
	store := DirStore{Root: t.TempDir()}
	defs := group("a.yaml", "g", 60, alert("A", "up == 0", 60, map[string]string{"sev": "page"}))

	first := snapAt(0, defs)
	if _, unchanged, err := Record(ctx, store, first); err != nil || unchanged {
		t.Fatalf("Record baseline: unchanged=%v, err=%v", unchanged, err)
	}

	for i, minute := range []int{5, 10} {
		again := snapAt(minute, defs)
		changes, unchanged, err := Record(ctx, store, again)
		if err != nil {
			t.Fatalf("Record identical capture at +%dm: %v", minute, err)
		}
		if !unchanged || len(changes) != 0 {
			t.Errorf("identical capture at +%dm: unchanged=%v changes=%v; want unchanged and none", minute, unchanged, summary(changes))
		}

		snaps, seen := tenantFiles(t, store, "team-a")
		if len(snaps) != 1 || len(seen) != 1 {
			t.Fatalf("after identical capture at +%dm: snapshots=%v seen=%v; want 1 of each", minute, snaps, seen)
		}

		latest, err := store.Latest(ctx, "team-a")
		if err != nil {
			t.Fatal(err)
		}
		if !latest.ObservedAt.Equal(first.ObservedAt) {
			t.Errorf("Latest is the snapshot observed at %s, want the baseline's %s", latest.ObservedAt, first.ObservedAt)
		}
		ls := latest.LastSeen
		if ls == nil || !ls.RequestedAt.Equal(again.RequestedAt) || !ls.ObservedAt.Equal(again.ObservedAt) || ls.Captures != i+2 {
			t.Errorf("after +%dm: LastSeen = %+v, want this capture's times and %d captures", minute, ls, i+2)
		}
	}

	if _, err := os.Stat(filepath.Join(store.Root, "team-a", changesFile)); !os.IsNotExist(err) {
		t.Errorf("changes file exists though nothing changed (err=%v)", err)
	}
}

func TestRecord_ChangeAfterUnchangedPeriod(t *testing.T) {
	ctx := context.Background()
	store := DirStore{Root: t.TempDir()}
	old := group("a.yaml", "g", 60, rec("x", "up"))
	captures := []Snapshot{snapAt(0, old), snapAt(5, old), snapAt(10, old)}
	for _, s := range captures {
		if _, _, err := Record(ctx, store, s); err != nil {
			t.Fatal(err)
		}
	}
	lastOld := captures[2]

	changed := snapAt(15, group("a.yaml", "g", 60, rec("x", "up == 1")))
	changes, unchanged, err := Record(ctx, store, changed)
	if err != nil || unchanged {
		t.Fatalf("Record changed capture: unchanged=%v, err=%v", unchanged, err)
	}
	if got := summary(changes); len(got) != 1 || got[0] != "changed team-a/a.yaml/g/x [expr]" {
		t.Fatalf("changes = %v", got)
	}
	// The window starts at the last capture that saw the old definitions,
	// not at the stored snapshot three captures back.
	want := Window{After: lastOld.RequestedAt, NotAfter: changed.ObservedAt}
	if changes[0].Window != want {
		t.Errorf("window = %+v, want %+v", changes[0].Window, want)
	}
	if snaps, _ := tenantFiles(t, store, "team-a"); len(snaps) != 2 {
		t.Errorf("snapshot files = %v, want the baseline and the changed one", snaps)
	}

	// The new snapshot is unchanged once more: its own LastSeen, and the old
	// one's stays where it was.
	tail := snapAt(20, group("a.yaml", "g", 60, rec("x", "up == 1")))
	if _, unchanged, err := Record(ctx, store, tail); err != nil || !unchanged {
		t.Fatalf("Record identical capture after the change: unchanged=%v, err=%v", unchanged, err)
	}

	// Rebuilding from the directory gives the same window, and says over
	// which spans nothing changed.
	snaps, err := ReadSnapshotDir(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	tl, err := Build(snaps)
	if err != nil {
		t.Fatal(err)
	}
	tt := tl.Tenants[0]
	if len(tt.Changes) != 1 || tt.Changes[0].Window != want {
		t.Errorf("Build changes = %+v, want one change in %+v", tt.Changes, want)
	}
	if tt.Snapshots != 2 || tt.Captures != 5 || !tt.LastObserved.Equal(tail.ObservedAt) {
		t.Errorf("Build coverage = %d snapshots, %d captures, last observed %s", tt.Snapshots, tt.Captures, tt.LastObserved)
	}
	wantSpans := []UnchangedSpan{
		{From: captures[0].ObservedAt, Through: lastOld.ObservedAt, Captures: 3},
		{From: changed.ObservedAt, Through: tail.ObservedAt, Captures: 2},
	}
	if !reflect.DeepEqual(tt.Unchanged, wantSpans) {
		t.Errorf("unchanged spans = %+v, want %+v", tt.Unchanged, wantSpans)
	}

	var md strings.Builder
	if err := WriteMarkdown(&md, tl); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		"5 captures stored as 2 snapshot(s)",
		"- Unchanged from 2026-09-19T03:00:02Z through 2026-09-19T03:10:02Z (3 captures).",
	} {
		if !strings.Contains(md.String(), line) {
			t.Errorf("markdown missing %q:\n%s", line, md.String())
		}
	}
}

func TestRecord_ReorderedDefinitionsStoreASnapshot(t *testing.T) {
	// Diff ignores order, so this is no change, but the file would differ:
	// it is stored rather than folded into the previous snapshot.
	ctx := context.Background()
	store := DirStore{Root: t.TempDir()}
	g1, g2 := group("a.yaml", "g1", 60, rec("x", "up")), group("a.yaml", "g2", 60, rec("y", "up"))
	if _, _, err := Record(ctx, store, snapAt(0, g1, g2)); err != nil {
		t.Fatal(err)
	}
	changes, unchanged, err := Record(ctx, store, snapAt(5, g2, g1))
	if err != nil || unchanged || len(changes) != 0 {
		t.Fatalf("reordered capture: changes=%v unchanged=%v err=%v; want a stored snapshot with no changes", summary(changes), unchanged, err)
	}
	if snaps, seen := tenantFiles(t, store, "team-a"); len(snaps) != 2 || len(seen) != 0 {
		t.Errorf("files: snapshots=%v seen=%v", snaps, seen)
	}
}

func TestRecord_RejectsCaptureOlderThanLastSeen(t *testing.T) {
	ctx := context.Background()
	store := DirStore{Root: t.TempDir()}
	for _, m := range []int{0, 10} {
		if _, _, err := Record(ctx, store, snapAt(m)); err != nil {
			t.Fatal(err)
		}
	}
	// After the baseline but before the last-seen capture.
	if _, _, err := Record(ctx, store, snapAt(5)); err == nil {
		t.Error("recording a capture older than the last-seen one: want error")
	}
}

func TestMarkSeen_RequiresItsSnapshot(t *testing.T) {
	store := DirStore{Root: t.TempDir()}
	s := snapAt(5)
	err := store.MarkSeen(context.Background(), LastSeen{
		Version: FormatVersion, Tenant: "team-a", Snapshot: snapAt(0).ObservedAt,
		Captures: 2, RequestedAt: s.RequestedAt, ObservedAt: s.ObservedAt,
	})
	if err == nil {
		t.Error("MarkSeen for a snapshot that was never stored: want error")
	}
}

func TestReadSnapshotDir_RejectsOrphanLastSeen(t *testing.T) {
	ctx := context.Background()
	store := DirStore{Root: t.TempDir()}
	for _, m := range []int{0, 5} {
		if _, _, err := Record(ctx, store, snapAt(m)); err != nil {
			t.Fatal(err)
		}
	}
	snaps, _ := tenantFiles(t, store, "team-a")
	if err := os.Remove(filepath.Join(store.Root, "team-a", snaps[0])); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSnapshotDir(store.Root); err == nil || !strings.Contains(err.Error(), "no snapshot") {
		t.Errorf("ReadSnapshotDir with a last-seen file and no snapshot: err = %v", err)
	}
}
