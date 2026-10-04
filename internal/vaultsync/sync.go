package vaultsync

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/faizahmd2/edrive/internal/config"
	"github.com/faizahmd2/edrive/internal/rclone"
)

// RemoteTrash is a folder inside the cloud vault that receives every file a
// sync would otherwise overwrite or remove. Cryptomator ignores it.
const RemoteTrash = ".edrive-trash"

type Syncer struct {
	RC         *rclone.Client
	LocalVault string
	Remote     string
	StatePath  string
	LocalTrash string
	// Progress, if set, is called as each step advances: label, files done, total.
	Progress func(label string, done, total int)
}

// step returns a per-file callback for one step and announces it.
func (s *Syncer) step(label string, total int) func(int) {
	if s.Progress == nil || total == 0 {
		return nil
	}
	s.Progress(label, 0, total)
	return func(done int) { s.Progress(label, done, total) }
}

type Result struct {
	Plan     Plan
	Uploaded int
	Fetched  int
	Trashed  int
}

// ErrIncomplete means one side is not a complete Cryptomator vault.
var ErrIncomplete = errors.New("vault is incomplete")

// Preview computes what a sync would do without changing anything.
func (s *Syncer) Preview() (Plan, error) {
	local, remote, prev, err := s.scan()
	if err != nil {
		return Plan{}, err
	}
	return MakePlan(local, remote, prev, false), nil
}

// Run brings both sides together and records a new snapshot.
func (s *Syncer) Run(allowMassDeletion bool) (Result, error) {
	local, remote, prev, err := s.scan()
	if err != nil {
		return Result{}, err
	}
	plan := MakePlan(local, remote, prev, allowMassDeletion)
	res := Result{Plan: plan}
	if plan.Empty() {
		// Nothing moved, so what we just listed is the new snapshot; this
		// saves a second (slow) cloud listing on every no-op sync.
		return res, s.saveSnapshot(prev, plan, local, remote)
	}

	stamp := time.Now().UTC().Format("20060102-150405")
	remoteTrash := s.Remote + "/" + RemoteTrash + "/" + stamp
	localTrash := filepath.Join(s.LocalTrash, stamp)

	for _, d := range plan.MkdirLocal {
		if err := os.MkdirAll(filepath.Join(s.LocalVault, filepath.FromSlash(d)), 0700); err != nil {
			return res, err
		}
	}
	mkdirs := s.step("creating empty folders", len(plan.MkdirRemote))
	for i, d := range plan.MkdirRemote {
		if err := s.RC.Mkdir(s.Remote + "/" + d); err != nil {
			return res, err
		}
		if mkdirs != nil {
			mkdirs(i + 1)
		}
	}
	if err := s.RC.Copy(s.LocalVault, s.Remote, plan.Upload, remoteTrash, s.step("uploading", len(plan.Upload))); err != nil {
		return res, err
	}
	res.Uploaded = len(plan.Upload)
	if len(plan.Download) > 0 {
		if err := os.MkdirAll(s.LocalTrash, 0700); err != nil {
			return res, err
		}
		if err := s.RC.Copy(s.Remote, s.LocalVault, plan.Download, localTrash, s.step("downloading", len(plan.Download))); err != nil {
			return res, err
		}
	}
	res.Fetched = len(plan.Download)
	if err := s.RC.Move(s.Remote, remoteTrash, plan.TrashRemote, s.step("moving removed files to cloud trash", len(plan.TrashRemote))); err != nil {
		return res, err
	}
	for _, p := range plan.TrashLocal {
		if err := moveToTrash(s.LocalVault, localTrash, p); err != nil {
			return res, err
		}
	}
	res.Trashed = len(plan.TrashRemote) + len(plan.TrashLocal)

	if err := s.recordSnapshot(prev, plan); err != nil {
		return res, err
	}
	return res, nil
}

// LocalChanged reports whether the local vault differs from the last
// snapshot. It needs no network, so it is cheap to call after every lock.
func (s *Syncer) LocalChanged() bool {
	prev, err := s.loadSnapshot()
	if err != nil || prev == nil {
		return true
	}
	local, err := ListLocal(s.LocalVault)
	if err != nil {
		return true
	}
	for p, e := range local {
		if e.Dir {
			continue
		}
		if b, ok := prev.Local[p]; !ok || !same(e, b) {
			return true
		}
	}
	for p, e := range prev.Local {
		if _, ok := local[p]; !ok && !e.Dir {
			return true
		}
	}
	return false
}

// LastSync returns when the last successful sync finished (zero if never).
func (s *Syncer) LastSync() time.Time {
	prev, err := s.loadSnapshot()
	if err != nil || prev == nil {
		return time.Time{}
	}
	return prev.At
}

func (s *Syncer) scan() (Manifest, Manifest, *Snapshot, error) {
	local, err := ListLocal(s.LocalVault)
	if err != nil {
		return nil, nil, nil, err
	}
	remote, err := s.listRemote()
	if err != nil {
		return nil, nil, nil, err
	}
	if len(local) > 0 && !looksComplete(local) {
		return nil, nil, nil, fmt.Errorf("%w on this Mac (%s)", ErrIncomplete, s.LocalVault)
	}
	if len(remote) > 0 && !looksComplete(remote) {
		return nil, nil, nil, fmt.Errorf("%w in the cloud (%s)", ErrIncomplete, s.Remote)
	}
	prev, err := s.loadSnapshot()
	if err != nil {
		return nil, nil, nil, err
	}
	return local, remote, prev, nil
}

// recordSnapshot re-lists both sides after the sync. Removals that were held
// back keep their old snapshot entry so they stay pending and are not
// silently forgotten.
func (s *Syncer) recordSnapshot(prev *Snapshot, plan Plan) error {
	local, err := ListLocal(s.LocalVault)
	if err != nil {
		return err
	}
	remote, err := s.listRemote()
	if err != nil {
		return err
	}
	return s.saveSnapshot(prev, plan, local, remote)
}

func (s *Syncer) saveSnapshot(prev *Snapshot, plan Plan, local, remote Manifest) error {
	if prev != nil && plan.HeldDeletions > 0 {
		for p, e := range prev.Local {
			if _, ok := local[p]; !ok {
				if _, inRemote := remote[p]; inRemote {
					local[p] = e
				}
			}
		}
		for p, e := range prev.Remote {
			if _, ok := remote[p]; !ok {
				if _, inLocal := local[p]; inLocal {
					remote[p] = e
				}
			}
		}
	}
	data, err := json.Marshal(Snapshot{Local: local, Remote: remote, At: time.Now().UTC()})
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(s.StatePath, data, 0600)
}

func (s *Syncer) loadSnapshot() (*Snapshot, error) {
	data, err := os.ReadFile(s.StatePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		// A damaged snapshot only costs us change history; the first-sync rules
		// (copy what is missing, never remove) are always safe.
		return nil, nil
	}
	return &snap, nil
}

func (s *Syncer) listRemote() (Manifest, error) {
	items, err := s.RC.List(s.Remote, true)
	if err != nil {
		return nil, err
	}
	m := Manifest{}
	for _, it := range items {
		if it.Path == RemoteTrash || strings.HasPrefix(it.Path, RemoteTrash+"/") || skipName(filepath.Base(it.Path)) {
			continue
		}
		m[it.Path] = Entry{Size: it.Size, ModTime: it.ModTime, Dir: it.IsDir}
	}
	return m, nil
}

// ListLocal walks the local encrypted vault.
func ListLocal(root string) (Manifest, error) {
	m := Manifest{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root && errors.Is(err, fs.ErrNotExist) {
				return fs.SkipAll
			}
			return err
		}
		if path == root {
			return nil
		}
		if skipName(d.Name()) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			m[rel] = Entry{Dir: true}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		m[rel] = Entry{Size: info.Size(), ModTime: info.ModTime()}
		return nil
	})
	return m, err
}

func skipName(name string) bool {
	return name == ".DS_Store" || strings.HasPrefix(name, "._")
}

func looksComplete(m Manifest) bool {
	_, mk := m["masterkey.cryptomator"]
	_, vf := m["vault.cryptomator"]
	return mk && vf
}

func moveToTrash(root, trash, rel string) error {
	src := filepath.Join(root, filepath.FromSlash(rel))
	dst := filepath.Join(trash, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
