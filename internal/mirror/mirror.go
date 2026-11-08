// Package mirror copies a folder of sealed secrets into the vault, one way.
//
// An app keeps each secret value as one age file on this Mac, named
// <id>.age with id like sec_…, encrypted to the app's own key and to a mirror
// key. The mirror key's private half lives only inside the vault
// (mirror/.sync/key.txt), so one vault unlock reads them all. Each value is
// written into the vault as mirror/<path>.txt, readable in the Cryptomator
// apps. The app on this Mac is the only place a secret changes; the vault is
// a copy.
//
// A sealed file holds UTF-8 JSON: {"v":1,"id":"sec_…","path":"a/b","value":"…"}
// (label, kind and sealed_at may ride along).
//
// edrive only reads the sealed folder: it never writes, renames or removes
// anything there. In the vault nothing is deleted: what a sync replaces or
// removes moves to mirror/.trash/<time>/.
package mirror

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"filippo.io/age"
)

const (
	SyncDir   = ".sync"  // inside the mirror: key.txt and state.json
	TrashDir  = ".trash" // inside the mirror: what a sync replaced or removed
	keyFile   = "key.txt"
	stateFile = "state.json"
	maxSealed = 1 << 20 // a sealed file over 1 MB is not a secret
)

var sealedName = regexp.MustCompile(`^(sec_[0-9A-Za-z]+)\.age$`)

// Secret is one sealed file, opened.
type Secret struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	Label    string `json:"label"`
	Kind     string `json:"kind"`
	Value    string `json:"value"`
	SealedAt string `json:"sealed_at"`
	V        int    `json:"v"`
	cipher   string // SHA-256 of the sealed file
}

// Entry is what the last sync wrote for one secret.
type Entry struct {
	Path   string `json:"path"`
	Cipher string `json:"cipher_sha256"`
	Plain  string `json:"plain_sha256"`
}

// State is mirror/.sync/state.json.
type State struct {
	LastSync time.Time        `json:"last_sync"`
	Secrets  map[string]Entry `json:"secrets"`
}

// Problem is a sealed file that could not be used. It is skipped; the rest go on.
type Problem struct {
	File   string
	Reason string
}

// ---- the mirror key ----

// Init creates the mirror key in the mirror unless it exists, and returns its
// public recipient (age1…). It never overwrites a key.
func Init(mirror string) (recipient string, created bool, err error) {
	at := filepath.Join(mirror, SyncDir, keyFile)
	if id, err := loadKey(at); err == nil {
		return id.Recipient().String(), false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", false, err
	}
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return "", false, err
	}
	if err := os.MkdirAll(filepath.Dir(at), 0700); err != nil {
		return "", false, err
	}
	body := fmt.Sprintf("# edrive mirror key, made on %s\n# public key: %s\n%s\n",
		time.Now().Format("02-01-2006"), id.Recipient(), id)
	f, err := os.OpenFile(at, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", false, err
	}
	if _, err := f.WriteString(body); err != nil {
		f.Close()
		return "", false, err
	}
	if err := f.Close(); err != nil {
		return "", false, err
	}
	return id.Recipient().String(), true, nil
}

func loadKey(at string) (*age.X25519Identity, error) {
	data, err := os.ReadFile(at)
	if err != nil {
		return nil, err
	}
	ids, err := age.ParseIdentities(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("the mirror key at %s is damaged: %w", at, err)
	}
	for _, id := range ids {
		if x, ok := id.(*age.X25519Identity); ok {
			return x, nil
		}
	}
	return nil, fmt.Errorf("the mirror key at %s holds no X25519 key", at)
}

// ---- reading the sealed folder ----

// Read opens every sealed file with the mirror key. It only reads.
func Read(sealed string, id age.Identity) ([]Secret, []Problem, error) {
	entries, err := os.ReadDir(sealed)
	if err != nil {
		return nil, nil, err
	}
	var secrets []Secret
	var problems []Problem
	for _, e := range entries {
		m := sealedName.FindStringSubmatch(e.Name())
		if e.IsDir() || m == nil {
			continue // .old/ and anything else is not ours
		}
		s, err := open(filepath.Join(sealed, e.Name()), id)
		if err == nil && s.ID != m[1] {
			err = fmt.Errorf("it says it is %s", s.ID)
		}
		if err != nil {
			problems = append(problems, Problem{e.Name(), err.Error()})
			continue
		}
		secrets = append(secrets, s)
	}
	sort.Slice(secrets, func(i, j int) bool { return secrets[i].ID < secrets[j].ID })
	return secrets, problems, nil
}

func open(file string, id age.Identity) (Secret, error) {
	f, err := os.Open(file)
	if err != nil {
		return Secret{}, err
	}
	defer f.Close()
	sealed, err := io.ReadAll(io.LimitReader(f, maxSealed+1))
	if err != nil {
		return Secret{}, err
	}
	if len(sealed) > maxSealed {
		return Secret{}, fmt.Errorf("it is over 1 MB")
	}
	r, err := age.Decrypt(bytes.NewReader(sealed), id)
	if err != nil {
		return Secret{}, fmt.Errorf("the mirror key cannot open it (%v)", err)
	}
	plain, err := io.ReadAll(r)
	if err != nil {
		return Secret{}, fmt.Errorf("it is damaged (%v)", err)
	}
	defer wipe(plain)
	var s Secret
	if err := json.Unmarshal(plain, &s); err != nil {
		return Secret{}, fmt.Errorf("it is not a sealed secret")
	}
	if s.V != 1 {
		return Secret{}, fmt.Errorf("format v%d is not known to this edrive", s.V)
	}
	if err := CheckPath(s.Path); err != nil {
		return Secret{}, err
	}
	s.cipher = sum(sealed)
	return s, nil
}

// CheckPath allows a plain relative path of names, none starting with a dot,
// so a secret can never land outside mirror/ or on .sync or .trash.
func CheckPath(p string) error {
	if p == "" || len(p) > 400 || !utf8.ValidString(p) {
		return fmt.Errorf("its path %q is not allowed", p)
	}
	if strings.HasPrefix(p, "/") || strings.Contains(p, "\\") || path.Clean(p) != p {
		return fmt.Errorf("its path %q is not a plain relative path", p)
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.ContainsAny(part, "\x00:") || len(part) > 200 {
			return fmt.Errorf("its path %q has a part that is not allowed: %q", p, part)
		}
	}
	return nil
}

// ---- the plan ----

// Plan is what a sync will do.
type Plan struct {
	New, Changed, Unchanged []Secret
	Moved                   map[string]string // id → the path it had
	Removed                 []string          // ids
	Clashes                 []Problem         // two secrets with one path: the later is skipped
	Refused                 string            // why nothing will be written, if anything
}

// MakePlan compares what is sealed now with what the last sync wrote. A file
// that could not be read is neither new nor removed: its last copy stays.
func MakePlan(secrets []Secret, problems []Problem, prev State, allowDeletes bool) Plan {
	p := Plan{Moved: map[string]string{}}
	seen := map[string]bool{}
	byPath := map[string]string{}
	for _, pr := range problems {
		if m := sealedName.FindStringSubmatch(pr.File); m != nil {
			seen[m[1]] = true
		}
	}
	for _, s := range secrets {
		seen[s.ID] = true
		if other, ok := byPath[s.Path]; ok {
			p.Clashes = append(p.Clashes, Problem{s.ID + ".age", "it has the same path as " + other})
			continue
		}
		byPath[s.Path] = s.ID
		was, ok := prev.Secrets[s.ID]
		switch {
		case !ok:
			p.New = append(p.New, s)
		case was.Cipher != s.cipher || was.Path != s.Path:
			p.Changed = append(p.Changed, s)
			if was.Path != s.Path {
				p.Moved[s.ID] = was.Path
			}
		default:
			p.Unchanged = append(p.Unchanged, s)
		}
	}
	for id := range prev.Secrets {
		if !seen[id] {
			p.Removed = append(p.Removed, id)
		}
	}
	sort.Strings(p.Removed)

	if !allowDeletes {
		had := len(prev.Secrets)
		switch {
		case had > 0 && len(secrets) == 0 && len(problems) == 0:
			p.Refused = fmt.Sprintf("the sealed folder is empty, but the vault holds %d secrets from the last sync", had)
		case len(p.Removed) >= 3 && len(p.Removed)*5 > had:
			p.Refused = fmt.Sprintf("%d of %d secrets would be removed at once", len(p.Removed), had)
		}
	}
	return p
}

// ---- applying it ----

// Result says what Apply did.
type Result struct {
	Written, Removed int
	HandEdited       []string // copies changed in the vault, kept in trash
	Total            int
}

// Apply writes the plan into the mirror and returns the new state. Nothing is
// deleted. It works in two steps, so secrets that swap paths, or a new secret
// taking a removed one's path, can never trash a file just written: first
// every file about to be replaced moves to trash, then every value is written.
func Apply(mirror string, plan Plan, prev State, now time.Time) (State, Result, error) {
	var res Result
	if plan.Refused != "" {
		return prev, res, fmt.Errorf("nothing copied: %s; if that is right, run again with --allow-deletes", plan.Refused)
	}
	file := func(rel string) string { return filepath.Join(mirror, filepath.FromSlash(rel)+".txt") }
	trash := filepath.Join(mirror, TrashDir, now.Format("20060102-150405"))
	toTrash := func(rel string) error {
		from := file(rel)
		if _, err := os.Lstat(from); errors.Is(err, os.ErrNotExist) {
			return nil
		}
		to := filepath.Join(trash, filepath.FromSlash(rel)+".txt")
		for n := 1; exists(to); n++ {
			to = filepath.Join(trash, fmt.Sprintf("%s.%d.txt", filepath.FromSlash(rel), n))
		}
		if err := os.MkdirAll(filepath.Dir(to), 0700); err != nil {
			return err
		}
		return os.Rename(from, to)
	}
	// A mirror file whose contents are not what the last sync wrote was
	// changed in the vault. The Mac's value wins; the edited copy is kept.
	handEdited := func(e Entry) (bool, error) {
		data, err := os.ReadFile(file(e.Path))
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		defer wipe(data)
		return sum(data) != e.Plain, nil
	}

	// Step 1: decide what gets written, and move aside what it replaces.
	var writes []Secret
	var vacate []string
	for _, id := range plan.Removed {
		vacate = append(vacate, prev.Secrets[id].Path)
	}
	for _, s := range plan.Changed {
		was := prev.Secrets[s.ID]
		if edited, err := handEdited(was); err != nil {
			return prev, res, err
		} else if edited {
			res.HandEdited = append(res.HandEdited, was.Path)
		}
		vacate = append(vacate, was.Path)
		writes = append(writes, s)
	}
	for _, s := range plan.Unchanged {
		was := prev.Secrets[s.ID]
		edited, err := handEdited(was)
		if err != nil {
			return prev, res, err
		}
		switch {
		case edited:
			res.HandEdited = append(res.HandEdited, was.Path)
			vacate = append(vacate, was.Path)
			writes = append(writes, s)
		case !exists(file(was.Path)):
			writes = append(writes, s)
		}
	}
	writes = append(writes, plan.New...)
	for _, rel := range vacate {
		if err := toTrash(rel); err != nil {
			return prev, res, err
		}
	}
	for _, s := range writes {
		if err := toTrash(s.Path); err != nil { // a file the mirror never wrote, on a path a secret now takes
			return prev, res, err
		}
	}

	// Step 2: write.
	next := State{LastSync: now, Secrets: map[string]Entry{}}
	for id, e := range prev.Secrets {
		next.Secrets[id] = e
	}
	for _, id := range plan.Removed {
		delete(next.Secrets, id)
		res.Removed++
	}
	for _, s := range writes {
		if err := writeAtomic(file(s.Path), []byte(s.Value)); err != nil {
			return prev, res, err
		}
		next.Secrets[s.ID] = Entry{Path: s.Path, Cipher: s.cipher, Plain: sum([]byte(s.Value))}
		res.Written++
	}
	res.Total = len(next.Secrets)
	return next, res, nil
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// ---- state ----

func LoadState(mirror string) (State, error) {
	s := State{Secrets: map[string]Entry{}}
	data, err := os.ReadFile(filepath.Join(mirror, SyncDir, stateFile))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("mirror/.sync/state.json is damaged: %w", err)
	}
	if s.Secrets == nil {
		s.Secrets = map[string]Entry{}
	}
	return s, nil
}

func SaveState(mirror string, s State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(mirror, SyncDir, stateFile), data)
}

// KeyPath is where the mirror key lives in the mirror.
func KeyPath(mirror string) string { return filepath.Join(mirror, SyncDir, keyFile) }

// LoadKey reads the mirror key from the mirror.
func LoadKey(mirror string) (*age.X25519Identity, error) {
	id, err := loadKey(KeyPath(mirror))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("there is no mirror key in the vault yet; run 'edrive mirror init'")
	}
	return id, err
}

func writeAtomic(at string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(at), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(at), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), at)
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
