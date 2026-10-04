// Package vaultsync keeps the local encrypted vault and the cloud copy in
// step without ever deleting data.
//
// Each side is compared with a snapshot taken after the last successful sync,
// so edrive knows which side changed a file. Changes on one side are copied to
// the other. If both sides changed the same file, the newer one wins and the
// older one is kept in a trash folder. Removals are moved to trash, never
// deleted, and a large share of files disappearing at once is never mirrored.
package vaultsync

import (
	"sort"
	"strings"
	"time"
)

// Entry describes one file or directory on one side.
type Entry struct {
	Size    int64     `json:"s"`
	ModTime time.Time `json:"m"`
	Dir     bool      `json:"d,omitempty"`
}

// Manifest maps slash-separated relative paths to entries.
type Manifest map[string]Entry

// Snapshot is what both sides looked like after the last successful sync.
type Snapshot struct {
	Local  Manifest  `json:"local"`
	Remote Manifest  `json:"remote"`
	At     time.Time `json:"at"`
}

// Plan lists the operations needed to bring both sides together.
type Plan struct {
	Upload        []string // copy local -> cloud
	Download      []string // copy cloud -> local
	TrashRemote   []string // removed locally: move the cloud copy to cloud trash
	TrashLocal    []string // removed in cloud: move the local copy to local trash
	MkdirRemote   []string
	MkdirLocal    []string
	Conflicts     int // files changed on both sides (newer kept, older in trash)
	HeldDeletions int // removals not mirrored because too much vanished at once
}

func (p Plan) Empty() bool {
	return len(p.Upload)+len(p.Download)+len(p.TrashRemote)+len(p.TrashLocal)+
		len(p.MkdirRemote)+len(p.MkdirLocal) == 0
}

// massDeletion: if more than this share of a side's files would be removed
// (and more than massDeletionMin files), removals are held back.
const (
	massDeletionShare = 0.5
	massDeletionMin   = 10
)

// MakePlan compares both sides against the previous snapshot (nil on the very
// first sync, in which case nothing is ever removed).
func MakePlan(local, remote Manifest, prev *Snapshot, allowMassDeletion bool) Plan {
	var baseL, baseR Manifest
	if prev != nil {
		baseL, baseR = prev.Local, prev.Remote
	}

	paths := map[string]struct{}{}
	for _, m := range []Manifest{local, remote, baseL, baseR} {
		for p := range m {
			paths[p] = struct{}{}
		}
	}
	sorted := make([]string, 0, len(paths))
	for p := range paths {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)

	var plan Plan
	for _, p := range sorted {
		l, inL := local[p]
		r, inR := remote[p]
		bl, inBL := baseL[p]
		br, inBR := baseR[p]

		if (inL && l.Dir) || (inR && r.Dir) {
			planDir(&plan, p, inL, inR, inBL, inBR)
			continue
		}

		lChanged := changed(l, inL, bl, inBL)
		rChanged := changed(r, inR, br, inBR)

		switch {
		case !lChanged && !rChanged:
		case lChanged && !rChanged:
			if inL {
				plan.Upload = append(plan.Upload, p)
			} else if inR {
				plan.TrashRemote = append(plan.TrashRemote, p)
			}
		case !lChanged && rChanged:
			if inR {
				plan.Download = append(plan.Download, p)
			} else if inL {
				plan.TrashLocal = append(plan.TrashLocal, p)
			}
		default: // changed on both sides
			switch {
			case inL && inR:
				if same(l, r) {
					continue
				}
				plan.Conflicts++
				if l.ModTime.After(r.ModTime) {
					plan.Upload = append(plan.Upload, p)
				} else {
					plan.Download = append(plan.Download, p)
				}
			case inL: // edited here, removed there: keep the edit
				plan.Upload = append(plan.Upload, p)
			case inR:
				plan.Download = append(plan.Download, p)
			}
		}
	}

	// Copying a file creates its parent folders, so only folders with nothing
	// inside need their own (slow, one-per-folder) mkdir.
	plan.MkdirRemote = onlyEmpty(plan.MkdirRemote, local)
	plan.MkdirLocal = onlyEmpty(plan.MkdirLocal, remote)

	if !allowMassDeletion {
		if tooMany(len(plan.TrashRemote), countFiles(remote)) {
			plan.HeldDeletions += len(plan.TrashRemote)
			plan.TrashRemote = nil
		}
		if tooMany(len(plan.TrashLocal), countFiles(local)) {
			plan.HeldDeletions += len(plan.TrashLocal)
			plan.TrashLocal = nil
		}
	}
	return plan
}

// Directories only matter when empty (Cryptomator keeps one per folder), and
// rclone creates parents for files automatically. New directories are
// mirrored; removed directories are left alone because an empty orphan
// directory is harmless.
func planDir(plan *Plan, p string, inL, inR, inBL, inBR bool) {
	switch {
	case inL && !inR && !inBR:
		plan.MkdirRemote = append(plan.MkdirRemote, p)
	case inR && !inL && !inBL:
		plan.MkdirLocal = append(plan.MkdirLocal, p)
	}
}

func onlyEmpty(dirs []string, source Manifest) []string {
	if len(dirs) == 0 {
		return nil
	}
	parents := map[string]struct{}{}
	for p := range source {
		for i := strings.LastIndexByte(p, '/'); i > 0; i = strings.LastIndexByte(p[:i], '/') {
			parents[p[:i]] = struct{}{}
		}
	}
	var out []string
	for _, d := range dirs {
		if _, hasChildren := parents[d]; !hasChildren {
			out = append(out, d)
		}
	}
	return out
}

func changed(cur Entry, inCur bool, base Entry, inBase bool) bool {
	if inCur != inBase {
		return true
	}
	if !inCur {
		return false
	}
	return !same(cur, base)
}

// same treats files as identical when size matches and modification times
// are within a second (some clouds store coarser timestamps).
func same(a, b Entry) bool {
	if a.Size != b.Size {
		return false
	}
	d := a.ModTime.Sub(b.ModTime)
	if d < 0 {
		d = -d
	}
	return d <= time.Second
}

// tooMany: total is the file count on the side the removals would apply to
// (those files are still there, so they are included in it).
func tooMany(removals, total int) bool {
	return removals > massDeletionMin && float64(removals) > massDeletionShare*float64(total)
}

func countFiles(m Manifest) int {
	n := 0
	for _, e := range m {
		if !e.Dir {
			n++
		}
	}
	return n
}
