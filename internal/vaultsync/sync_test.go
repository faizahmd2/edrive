package vaultsync

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/faizahmd2/edrive/internal/rclone"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func f(size int64, minutes int) Entry {
	return Entry{Size: size, ModTime: t0.Add(time.Duration(minutes) * time.Minute)}
}

func vault(extra map[string]Entry) Manifest {
	m := Manifest{"masterkey.cryptomator": f(1, 0), "vault.cryptomator": f(1, 0)}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func TestPlanOneSidedChanges(t *testing.T) {
	base := vault(map[string]Entry{"a": f(1, 0), "b": f(1, 0), "c": f(1, 0)})
	prev := &Snapshot{Local: base, Remote: base}

	local := vault(map[string]Entry{"a": f(2, 5), "b": f(1, 0), "new-local": f(1, 1)}) // a edited, c removed
	remote := vault(map[string]Entry{"a": f(1, 0), "b": f(3, 4), "c": f(1, 0), "new-remote": f(1, 1)})

	p := MakePlan(local, remote, prev, false)
	if !reflect.DeepEqual(p.Upload, []string{"a", "new-local"}) {
		t.Errorf("upload = %v", p.Upload)
	}
	if !reflect.DeepEqual(p.Download, []string{"b", "new-remote"}) {
		t.Errorf("download = %v", p.Download)
	}
	if !reflect.DeepEqual(p.TrashRemote, []string{"c"}) {
		t.Errorf("trash remote = %v", p.TrashRemote)
	}
	if len(p.TrashLocal) != 0 || p.Conflicts != 0 {
		t.Errorf("unexpected plan %+v", p)
	}
}

func TestPlanConflictNewestWins(t *testing.T) {
	base := vault(map[string]Entry{"x": f(1, 0), "y": f(1, 0)})
	prev := &Snapshot{Local: base, Remote: base}
	local := vault(map[string]Entry{"x": f(2, 10), "y": f(2, 1)})
	remote := vault(map[string]Entry{"x": f(3, 5), "y": f(3, 7)})

	p := MakePlan(local, remote, prev, false)
	if p.Conflicts != 2 || !reflect.DeepEqual(p.Upload, []string{"x"}) || !reflect.DeepEqual(p.Download, []string{"y"}) {
		t.Errorf("plan = %+v", p)
	}
}

func TestPlanEditBeatsRemoval(t *testing.T) {
	base := vault(map[string]Entry{"x": f(1, 0)})
	prev := &Snapshot{Local: base, Remote: base}
	local := vault(map[string]Entry{"x": f(2, 3)})
	remote := vault(nil)

	p := MakePlan(local, remote, prev, false)
	if !reflect.DeepEqual(p.Upload, []string{"x"}) || len(p.TrashLocal) != 0 {
		t.Errorf("plan = %+v", p)
	}
}

func TestPlanFirstSyncNeverRemoves(t *testing.T) {
	local := vault(map[string]Entry{"only-local": f(1, 0), "both": f(1, 0)})
	remote := vault(map[string]Entry{"only-remote": f(1, 0), "both": f(1, 0)})

	p := MakePlan(local, remote, nil, false)
	if !reflect.DeepEqual(p.Upload, []string{"only-local"}) || !reflect.DeepEqual(p.Download, []string{"only-remote"}) {
		t.Errorf("plan = %+v", p)
	}
	if len(p.TrashLocal)+len(p.TrashRemote) != 0 {
		t.Errorf("first sync must never remove: %+v", p)
	}
}

func TestPlanHoldsMassDeletion(t *testing.T) {
	files := map[string]Entry{}
	for i := 0; i < 30; i++ {
		files[string(rune('a'+i%26))+string(rune('0'+i/26))] = f(1, 0)
	}
	base := vault(files)
	prev := &Snapshot{Local: base, Remote: base}

	p := MakePlan(vault(nil), base, prev, false)
	if len(p.TrashRemote) != 0 || p.HeldDeletions != 30 {
		t.Errorf("mass deletion not held: %+v", p)
	}
	p = MakePlan(vault(nil), base, prev, true)
	if len(p.TrashRemote) != 30 {
		t.Errorf("allowed mass deletion not planned: %d", len(p.TrashRemote))
	}
}

// TestRunAgainstLocalRemote exercises the real rclone binary with a local
// directory standing in for the cloud.
func TestRunAgainstLocalRemote(t *testing.T) {
	rcPath, err := exec.LookPath("rclone")
	if err != nil {
		t.Skip("rclone not installed")
	}
	dir := t.TempDir()
	localVault := filepath.Join(dir, "vault")
	remote := filepath.Join(dir, "cloud")
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(localVault, "masterkey.cryptomator"), "mk")
	write(filepath.Join(localVault, "vault.cryptomator"), "vf")
	write(filepath.Join(localVault, "d", "AA", "file.c9r"), "one")
	if err := os.MkdirAll(filepath.Join(localVault, "d", "BB", "empty"), 0700); err != nil {
		t.Fatal(err)
	}

	rc := &rclone.Client{Path: rcPath, RemoteName: "unused", RemotePath: "unused"}
	s := &Syncer{RC: rc, LocalVault: localVault, Remote: remote, StatePath: filepath.Join(dir, "state.json"), LocalTrash: filepath.Join(dir, "trash")}

	progress := map[string]int{}
	s.Progress = func(label string, done, total int) { progress[label] = done }
	if _, err := s.Run(false); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if progress["uploading"] != 3 {
		t.Fatalf("progress = %v, want 3 uploads counted", progress)
	}
	if _, err := os.Stat(filepath.Join(remote, "d", "BB", "empty")); err != nil {
		t.Fatalf("empty directory not mirrored: %v", err)
	}
	if s.LocalChanged() {
		t.Fatal("no local changes expected right after sync")
	}

	// Phone edits a file in the cloud; Mac removes another locally.
	time.Sleep(1100 * time.Millisecond)
	write(filepath.Join(remote, "d", "AA", "file.c9r"), "two-from-phone")
	write(filepath.Join(localVault, "d", "AA", "gone.c9r"), "x")
	if _, err := s.Run(false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(localVault, "d", "AA", "gone.c9r")); err != nil {
		t.Fatal(err)
	}
	if !s.LocalChanged() {
		t.Fatal("local removal not detected")
	}
	res, err := s.Run(false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Trashed != 1 {
		t.Fatalf("trashed = %d", res.Trashed)
	}
	if b, _ := os.ReadFile(filepath.Join(localVault, "d", "AA", "file.c9r")); string(b) != "two-from-phone" {
		t.Fatalf("cloud edit not pulled: %q", b)
	}
	if _, err := os.Stat(filepath.Join(remote, "d", "AA", "gone.c9r")); !os.IsNotExist(err) {
		t.Fatal("removed file still live in cloud")
	}
	matches, _ := filepath.Glob(filepath.Join(remote, RemoteTrash, "*", "d", "AA", "gone.c9r"))
	if len(matches) != 1 {
		t.Fatal("removed file was not kept in cloud trash")
	}
	plan, err := s.Preview()
	if err != nil || !plan.Empty() {
		t.Fatalf("expected nothing left to sync: %+v %v", plan, err)
	}
}

func TestPlanOnlyMkdirsEmptyFolders(t *testing.T) {
	local := vault(map[string]Entry{
		"d/AA":        {Dir: true},
		"d/AA/full":   {Dir: true},
		"d/AA/full/x": f(1, 0),
		"d/AA/empty":  {Dir: true},
	})
	p := MakePlan(local, vault(nil), nil, false)
	if !reflect.DeepEqual(p.MkdirRemote, []string{"d/AA/empty"}) {
		t.Errorf("mkdir = %v", p.MkdirRemote)
	}
}
