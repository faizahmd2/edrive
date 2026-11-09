package mirror

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
)

// world is a sealed folder on "the Mac" and a mirror in "the vault".
type world struct {
	t      *testing.T
	sealed string
	mirror string
	key    *age.X25519Identity
	mac    *age.X25519Identity // stands in for the app's own key
	clock  time.Time
}

func newWorld(t *testing.T) *world {
	root := t.TempDir()
	w := &world{t: t, sealed: filepath.Join(root, "sealed"), mirror: filepath.Join(root, "vault", "mirror"), clock: time.Date(2026, 11, 8, 18, 0, 0, 0, time.UTC)}
	_ = os.MkdirAll(w.sealed, 0700)
	recipient, created, err := Init(w.mirror)
	if err != nil || !created {
		t.Fatalf("init: %v %v", created, err)
	}
	if w.key, err = LoadKey(w.mirror); err != nil || w.key.Recipient().String() != recipient {
		t.Fatalf("load key: %v", err)
	}
	w.mac, _ = age.GenerateX25519Identity()
	return w
}

func (w *world) seal(id, path, value string) {
	w.t.Helper()
	body, _ := json.Marshal(map[string]any{"v": 1, "id": id, "path": path, "label": path, "kind": "password", "value": value, "sealed_at": "2026-11-08T18:00:00+05:30"})
	var out bytes.Buffer
	enc, err := age.Encrypt(&out, w.mac.Recipient(), w.key.Recipient())
	if err != nil {
		w.t.Fatal(err)
	}
	_, _ = enc.Write(body)
	_ = enc.Close()
	if err := os.WriteFile(filepath.Join(w.sealed, id+".age"), out.Bytes(), 0600); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) unseal(id string) { _ = os.Remove(filepath.Join(w.sealed, id+".age")) }

// sync runs one whole sync and returns the plan and result.
func (w *world) sync(allowDeletes bool) (Plan, Result, error) {
	w.t.Helper()
	w.clock = w.clock.Add(time.Minute)
	secrets, problems, err := Read(w.sealed, w.key)
	if err != nil {
		w.t.Fatal(err)
	}
	prev, err := LoadState(w.mirror)
	if err != nil {
		w.t.Fatal(err)
	}
	plan := MakePlan(secrets, problems, prev, allowDeletes)
	next, res, err := Apply(w.mirror, plan, prev, w.clock)
	if err == nil {
		err = SaveState(w.mirror, next)
	}
	return plan, res, err
}

func (w *world) has(path, value string) {
	w.t.Helper()
	b, err := os.ReadFile(filepath.Join(w.mirror, path+".txt"))
	if err != nil || string(b) != value {
		w.t.Errorf("%s: got %q, %v; want %q", path, b, err, value)
	}
}

func (w *world) lacks(path string) {
	w.t.Helper()
	if _, err := os.Stat(filepath.Join(w.mirror, path+".txt")); err == nil {
		w.t.Errorf("%s should not be in the mirror", path)
	}
}

// trashed lists every value kept in trash.
func (w *world) trashed() []string {
	var out []string
	_ = filepath.WalkDir(filepath.Join(w.mirror, TrashDir), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := os.ReadFile(p)
			out = append(out, string(b))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func TestRoundTripEditsMovesAndRemovals(t *testing.T) {
	w := newWorld(t)
	w.seal("sec_A", "self/bank/sbi-savings/account-number", "30001234567")
	w.seal("sec_B", "self/email/gmail/password", "old-gmail")
	w.seal("sec_C", "self/wifi/home", "jio-fiber-pass")
	w.seal("sec_D", "self/notes/locker", "blue tiger")
	w.seal("sec_E", "self/notes/हिंदी", "नमस्ते")
	if _, res, err := w.sync(false); err != nil || res.Written != 5 || res.Total != 5 {
		t.Fatalf("first sync: %+v %v", res, err)
	}
	w.has("self/bank/sbi-savings/account-number", "30001234567")
	w.has("self/notes/हिंदी", "नमस्ते")

	// Nothing changed: nothing written.
	if _, res, err := w.sync(false); err != nil || res.Written != 0 || res.Removed != 0 {
		t.Fatalf("idle sync: %+v %v", res, err)
	}

	w.seal("sec_B", "self/email/gmail/password", "new-gmail") // edited
	w.seal("sec_C", "self/wifi/office", "jio-fiber-pass")     // moved
	w.unseal("sec_D")                                         // removed
	w.seal("sec_F", "self/notes/locker", "a new secret on the removed one's path")
	if _, res, err := w.sync(false); err != nil || res.Written != 3 || res.Removed != 1 || res.Total != 5 {
		t.Fatalf("second sync: %+v %v", res, err)
	}
	w.has("self/email/gmail/password", "new-gmail")
	w.has("self/wifi/office", "jio-fiber-pass")
	w.lacks("self/wifi/home")
	w.has("self/notes/locker", "a new secret on the removed one's path")
	if got := w.trashed(); strings.Join(got, "|") != "blue tiger|jio-fiber-pass|old-gmail" {
		t.Fatalf("trash holds %q", got)
	}
}

func TestSwappedPathsBothLand(t *testing.T) {
	w := newWorld(t)
	w.seal("sec_A", "x", "value of A")
	w.seal("sec_B", "y", "value of B")
	if _, _, err := w.sync(false); err != nil {
		t.Fatal(err)
	}
	w.seal("sec_A", "y", "value of A")
	w.seal("sec_B", "x", "value of B")
	if _, _, err := w.sync(false); err != nil {
		t.Fatal(err)
	}
	w.has("x", "value of B")
	w.has("y", "value of A")
}

func TestMassRemovalIsRefused(t *testing.T) {
	w := newWorld(t)
	for i := 0; i < 6; i++ {
		w.seal(fmt.Sprintf("sec_%d", i), fmt.Sprintf("p/%d", i), "v")
	}
	if _, _, err := w.sync(false); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		w.unseal(fmt.Sprintf("sec_%d", i))
	}
	plan, _, err := w.sync(false)
	if err == nil || plan.Refused == "" {
		t.Fatal("3 of 6 removed at once must be refused")
	}
	w.has("p/0", "v") // nothing touched
	if _, res, err := w.sync(true); err != nil || res.Removed != 3 {
		t.Fatalf("--allow-deletes: %+v %v", res, err)
	}
	w.lacks("p/0")
	if len(w.trashed()) != 3 {
		t.Fatal("removed values must be kept in trash")
	}
}

func TestEmptySealedFolderIsRefused(t *testing.T) {
	w := newWorld(t)
	w.seal("sec_A", "a", "v")
	if _, _, err := w.sync(false); err != nil {
		t.Fatal(err)
	}
	w.unseal("sec_A")
	if _, _, err := w.sync(false); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("got %v", err)
	}
	w.has("a", "v")
}

// One broken file costs one secret its update, never the others, and is not taken for a removal.
func TestDamagedFileIsSkippedNotRemoved(t *testing.T) {
	w := newWorld(t)
	w.seal("sec_A", "a", "first")
	w.seal("sec_B", "b", "second")
	if _, _, err := w.sync(false); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(w.sealed, "sec_A.age"), []byte("not age at all"), 0600)
	w.seal("sec_B", "b", "second, edited")
	w.seal("sec_C", "../../escape", "x") // a path that climbs out
	w.seal("sec_D", "d", "y")
	_ = os.Rename(filepath.Join(w.sealed, "sec_D.age"), filepath.Join(w.sealed, "sec_Z.age")) // says it is another
	secrets, problems, err := Read(w.sealed, w.key)
	if err != nil || len(secrets) != 1 || len(problems) != 3 {
		t.Fatalf("read: %d secrets, problems %v, %v", len(secrets), problems, err)
	}
	if _, res, err := w.sync(false); err != nil || res.Removed != 0 {
		t.Fatalf("sync: %+v %v", res, err)
	}
	w.has("a", "first")
	w.has("b", "second, edited")
	if _, err := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(w.mirror)), "escape.txt")); err == nil {
		t.Fatal("a path must never climb out of the mirror")
	}
}

func TestHandEditedMirrorIsKeptAndRestored(t *testing.T) {
	w := newWorld(t)
	w.seal("sec_A", "a", "from the mac")
	if _, _, err := w.sync(false); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(w.mirror, "a.txt"), []byte("edited on a phone"), 0600)
	_, res, err := w.sync(false)
	if err != nil || len(res.HandEdited) != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	w.has("a", "from the mac")
	if got := w.trashed(); len(got) != 1 || got[0] != "edited on a phone" {
		t.Fatalf("trash holds %q", got)
	}
}

func TestReadNeverChangesTheSealedFolder(t *testing.T) {
	w := newWorld(t)
	w.seal("sec_A", "a", "v")
	_ = os.MkdirAll(filepath.Join(w.sealed, ".old"), 0700)
	_ = os.WriteFile(filepath.Join(w.sealed, ".old", "sec_A.202611081800.age"), []byte("old"), 0600)
	snap := func() string {
		var b strings.Builder
		_ = filepath.WalkDir(w.sealed, func(p string, d fs.DirEntry, err error) error {
			info, _ := d.Info()
			fmt.Fprintf(&b, "%s %d %v %v\n", p, info.Size(), info.ModTime(), info.Mode())
			return nil
		})
		return b.String()
	}
	before := snap()
	for i := 0; i < 2; i++ {
		if _, _, err := w.sync(false); err != nil {
			t.Fatal(err)
		}
	}
	if snap() != before {
		t.Fatal("a sync changed the sealed folder")
	}
}

func TestInitNeverOverwritesTheKey(t *testing.T) {
	w := newWorld(t)
	again, created, err := Init(w.mirror)
	if err != nil || created || again != w.key.Recipient().String() {
		t.Fatalf("second init: %v %v %v", again, created, err)
	}
	info, _ := os.Stat(KeyPath(w.mirror))
	if info.Mode().Perm() != 0600 {
		t.Fatalf("key file mode %v", info.Mode())
	}
}

func TestCheckPath(t *testing.T) {
	ok := []string{"a", "self/bank/sbi", "self/notes/हिंदी नोट", "a b/c-d_e"}
	bad := []string{"", "/abs", "../up", "a/../b", "a//b", "a/", ".sync/key", "a/.trash/x", "a\\b", "a:b", strings.Repeat("x", 201)}
	for _, p := range ok {
		if err := CheckPath(p); err != nil {
			t.Errorf("%q refused: %v", p, err)
		}
	}
	for _, p := range bad {
		if CheckPath(p) == nil {
			t.Errorf("%q allowed", p)
		}
	}
}

// A whole file (format v2) lands under its own name: mirror/<path>/<name>, byte for byte.
func TestWholeFileLandsUnderItsName(t *testing.T) {
	w := newWorld(t)
	pdf := []byte("%PDF-1.4 a scan\x00\x01\x02 with binary bytes")
	sealFile := func(id, path, name string, data []byte) {
		w.t.Helper()
		body, _ := json.Marshal(map[string]any{"v": 2, "id": id, "path": path, "file": map[string]string{"name": name, "b64": base64.StdEncoding.EncodeToString(data)}})
		var out bytes.Buffer
		enc, _ := age.Encrypt(&out, w.mac.Recipient(), w.key.Recipient())
		_, _ = enc.Write(body)
		_ = enc.Close()
		_ = os.WriteFile(filepath.Join(w.sealed, id+".age"), out.Bytes(), 0600)
	}
	sealFile("sec_F", "faiz/aadhaar-card", "aadhaar scan.pdf", pdf)
	w.seal("sec_V", "faiz/locker", "blue tiger")
	if _, res, err := w.sync(false); err != nil || res.Written != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	got, err := os.ReadFile(filepath.Join(w.mirror, "faiz", "aadhaar-card", "aadhaar scan.pdf"))
	if err != nil || !bytes.Equal(got, pdf) {
		t.Fatalf("file: %v %q", err, got)
	}
	w.has("faiz/locker", "blue tiger")
	// a new scan replaces it; the old one waits in trash
	sealFile("sec_F", "faiz/aadhaar-card", "aadhaar scan.pdf", []byte("%PDF-1.4 a new scan"))
	if _, _, err := w.sync(false); err != nil {
		t.Fatal(err)
	}
	if got := w.trashed(); len(got) != 1 || !bytes.Equal([]byte(got[0]), pdf) {
		t.Fatalf("trash holds %q", got)
	}
	// a name that climbs out is refused by name; the rest goes on
	sealFile("sec_X", "faiz/x", "../escape.pdf", pdf)
	_, problems, _ := Read(w.sealed, w.key)
	if len(problems) != 1 || !strings.Contains(problems[0].Reason, "file name") {
		t.Fatalf("problems %v", problems)
	}
}
