package cryptomator

// Reading a Cryptomator vault with nothing but its password: no Cryptomator
// app, no CLI, no FUSE, no network. This is what keeps the vault readable if
// Cryptomator ever disappears. Only vault format 8 with the SIV_GCM cipher
// combo is read; anything else is refused by name.
//
//	masterkey.cryptomator  scrypt(password) unwraps the encryption and MAC keys
//	vault.cryptomator      a JWT (HS256 with enc‖mac): format, cipher combo
//	d/XX/YYYY…/            one folder per directory, named from its id
//	<name>.c9r             a file, or a folder holding dir.c9r / symlink.c9r
//	<hash>.c9s/            a long name: name.c9s holds it, contents.c9r the file
//
// See https://docs.cryptomator.org/security/architecture/

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	headerSize    = 68
	nonceSize     = 12
	tagSize       = 16
	chunkPlain    = 32 * 1024
	chunkCipher   = nonceSize + chunkPlain + tagSize
	maxDirDepth   = 256
	cipherSIVGCM  = "SIV_GCM"
	vaultFormat   = 8
	masterkeyKind = "masterkeyfile:"
)

// ErrCorrupt means a part of the vault could not be authenticated or parsed.
var ErrCorrupt = errors.New("vault data is damaged")

// NodeKind says what a decoded entry is.
type NodeKind int

const (
	File NodeKind = iota
	Dir
	Symlink
)

// Node is one entry of the vault, by its plain path.
type Node struct {
	Path   string // slash-separated, relative to the vault root, never empty
	Kind   NodeKind
	Target string // for a Symlink
	src    string // the encrypted contents file, for a File
}

// Vault is an opened vault. Close wipes its keys.
type Vault struct {
	root string
	enc  []byte
	mac  []byte
}

// OpenVault checks the password and the vault config, and returns a vault
// ready to read.
func OpenVault(root string, password []byte) (*Vault, error) {
	token, err := os.ReadFile(filepath.Join(root, "vault.cryptomator"))
	if err != nil {
		return nil, fmt.Errorf("read vault config: %w", err)
	}
	parts := strings.Split(strings.TrimSpace(string(token)), ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("vault config is not a JWT")
	}
	var header struct {
		Kid string `json:"kid"`
		Alg string `json:"alg"`
	}
	if err := decodeJWTPart(parts[0], &header); err != nil {
		return nil, err
	}
	if header.Alg != "HS256" {
		return nil, fmt.Errorf("vault config is signed with %q; only HS256 is supported", header.Alg)
	}
	if header.Kid != masterkeyKind+"masterkey.cryptomator" {
		return nil, fmt.Errorf("vault key %q is not supported; only masterkey.cryptomator", header.Kid)
	}

	enc, mac, err := unlockMasterkey(root, password, true)
	if err != nil {
		return nil, err
	}
	v := &Vault{root: root, enc: enc, mac: mac}

	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		v.Close()
		return nil, fmt.Errorf("vault config signature is invalid")
	}
	sign := hmac.New(sha256.New, append(append([]byte(nil), enc...), mac...))
	sign.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(sign.Sum(nil), signature) {
		v.Close()
		return nil, fmt.Errorf("vault config does not match its key: %w", ErrCorrupt)
	}

	var payload struct {
		Format      int    `json:"format"`
		CipherCombo string `json:"cipherCombo"`
	}
	if err := decodeJWTPart(parts[1], &payload); err != nil {
		v.Close()
		return nil, err
	}
	if payload.Format != vaultFormat || payload.CipherCombo != cipherSIVGCM {
		v.Close()
		return nil, fmt.Errorf("vault format %d with %s is not supported; only format 8 with SIV_GCM", payload.Format, payload.CipherCombo)
	}
	return v, nil
}

func decodeJWTPart(part string, into any) error {
	data, err := base64.RawURLEncoding.DecodeString(part)
	if err != nil {
		return fmt.Errorf("vault config is not valid base64")
	}
	if err := json.Unmarshal(data, into); err != nil {
		return fmt.Errorf("vault config is not valid JSON")
	}
	return nil
}

// Close wipes the keys.
func (v *Vault) Close() {
	wipe(v.enc)
	wipe(v.mac)
}

// dirPath is where the directory with this id keeps its entries.
func (v *Vault) dirPath(dirID string) (string, error) {
	siv, err := sivEncrypt(v.mac, v.enc, []byte(dirID))
	if err != nil {
		return "", err
	}
	sum := sha1.Sum(siv)
	hash := base32.StdEncoding.EncodeToString(sum[:])
	return filepath.Join(v.root, "d", hash[:2], hash[2:]), nil
}

func (v *Vault) decryptName(encrypted, dirID string) (string, error) {
	encrypted = strings.TrimSuffix(encrypted, ".c9r")
	raw, err := base64.URLEncoding.DecodeString(encrypted)
	if err != nil {
		raw, err = base64.RawURLEncoding.DecodeString(encrypted)
	}
	if err != nil {
		return "", fmt.Errorf("a file name is not valid base64: %w", ErrCorrupt)
	}
	name, err := sivDecrypt(v.mac, v.enc, raw, []byte(dirID))
	if err != nil {
		return "", fmt.Errorf("a file name failed its check: %w", ErrCorrupt)
	}
	s := string(name)
	if s == "" || s == "." || s == ".." || strings.ContainsAny(s, "/\x00") {
		return "", fmt.Errorf("a file name %q is not allowed: %w", s, ErrCorrupt)
	}
	return s, nil
}

// Walk visits every entry, parents before children, names in order. fn may
// return fs.SkipDir on a Dir to skip what is inside it.
func (v *Vault) Walk(fn func(Node) error) error {
	return v.walk("", "", map[string]bool{}, 0, fn)
}

func (v *Vault) walk(dirID, prefix string, seen map[string]bool, depth int, fn func(Node) error) error {
	if seen[dirID] {
		return fmt.Errorf("a folder contains itself: %w", ErrCorrupt)
	}
	if depth > maxDirDepth {
		return fmt.Errorf("folders are nested too deep: %w", ErrCorrupt)
	}
	seen[dirID] = true
	dir, err := v.dirPath(dirID)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if dirID != "" && errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("folder %q is missing its contents: %w", prefix, ErrCorrupt)
		}
		return err
	}

	type item struct {
		node  Node
		child string
	}
	var items []item
	for _, e := range entries {
		at := filepath.Join(dir, e.Name())
		var encrypted string
		switch {
		case strings.HasSuffix(e.Name(), ".c9s") && e.IsDir():
			long, err := os.ReadFile(filepath.Join(at, "name.c9s"))
			if err != nil {
				return fmt.Errorf("a long name in %q is unreadable: %w", prefix, ErrCorrupt)
			}
			encrypted = strings.TrimSpace(string(long))
		case strings.HasSuffix(e.Name(), ".c9r"):
			if e.Name() == "dirid.c9r" { // a backup of this folder's own id
				continue
			}
			encrypted = e.Name()
		default:
			continue
		}
		name, err := v.decryptName(encrypted, dirID)
		if err != nil {
			return fmt.Errorf("in %q: %w", prefix, err)
		}
		node, child, err := v.nodeAt(at, e.IsDir(), path.Join(prefix, name))
		if err != nil {
			return err
		}
		items = append(items, item{node, child})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].node.Path < items[j].node.Path })

	for _, it := range items {
		err := fn(it.node)
		if errors.Is(err, fs.SkipDir) && it.node.Kind == Dir {
			continue
		}
		if err != nil {
			return err
		}
		if it.node.Kind == Dir {
			if err := v.walk(it.child, it.node.Path, seen, depth+1, fn); err != nil {
				return err
			}
		}
	}
	return nil
}

// nodeAt reads what one .c9r or .c9s entry is. For a Dir it also returns the
// child directory id.
func (v *Vault) nodeAt(at string, isDir bool, plain string) (Node, string, error) {
	if !isDir {
		return Node{Path: plain, Kind: File, src: at}, "", nil
	}
	if id, err := os.ReadFile(filepath.Join(at, "dir.c9r")); err == nil {
		return Node{Path: plain, Kind: Dir}, string(id), nil
	}
	if _, err := os.Stat(filepath.Join(at, "symlink.c9r")); err == nil {
		target, err := v.readAll(filepath.Join(at, "symlink.c9r"))
		if err != nil {
			return Node{}, "", fmt.Errorf("link %q: %w", plain, err)
		}
		return Node{Path: plain, Kind: Symlink, Target: string(target)}, "", nil
	}
	if _, err := os.Stat(filepath.Join(at, "contents.c9r")); err == nil {
		return Node{Path: plain, Kind: File, src: filepath.Join(at, "contents.c9r")}, "", nil
	}
	return Node{}, "", fmt.Errorf("%q is neither a file, a folder nor a link: %w", plain, ErrCorrupt)
}

func (v *Vault) readAll(src string) ([]byte, error) {
	r, err := v.openContents(src)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// Open returns the plain contents of a File.
func (v *Vault) Open(n Node) (io.ReadCloser, error) {
	if n.Kind != File {
		return nil, fmt.Errorf("%q is not a file", n.Path)
	}
	r, err := v.openContents(n.src)
	if err != nil {
		return nil, fmt.Errorf("%q: %w", n.Path, err)
	}
	return r, nil
}

// contentReader decrypts a file chunk by chunk: a 68-byte header holding the
// file's own key, then chunks of nonce, up to 32 KiB of AES-GCM ciphertext
// and a tag. Each chunk is bound to its number and to the header.
type contentReader struct {
	f       *os.File
	aead    cipher.AEAD
	nonce   []byte
	chunk   uint64
	buf     []byte
	pending []byte
	done    bool
}

func (v *Vault) openContents(src string) (*contentReader, error) {
	f, err := os.Open(src)
	if err != nil {
		return nil, err
	}
	header := make([]byte, headerSize)
	if n, err := io.ReadFull(f, header); err != nil {
		if n == 0 && err == io.EOF { // an empty file Cryptomator never wrote a header for
			return &contentReader{f: f, done: true}, nil
		}
		f.Close()
		return nil, fmt.Errorf("file header is cut short: %w", ErrCorrupt)
	}
	headerAEAD, err := newGCM(v.enc)
	if err != nil {
		f.Close()
		return nil, err
	}
	payload, err := headerAEAD.Open(nil, header[:nonceSize], header[nonceSize:], nil)
	if err != nil || len(payload) != 40 {
		f.Close()
		return nil, fmt.Errorf("file header failed its check: %w", ErrCorrupt)
	}
	defer wipe(payload)
	aead, err := newGCM(payload[8:])
	if err != nil {
		f.Close()
		return nil, err
	}
	return &contentReader{f: f, aead: aead, nonce: header[:nonceSize], buf: make([]byte, chunkCipher)}, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (r *contentReader) Read(p []byte) (int, error) {
	for len(r.pending) == 0 {
		if r.done {
			return 0, io.EOF
		}
		if err := r.next(); err != nil {
			return 0, err
		}
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

func (r *contentReader) next() error {
	n, err := io.ReadFull(r.f, r.buf)
	switch {
	case err == io.EOF:
		r.done = true
		return nil
	case err == io.ErrUnexpectedEOF:
		r.done = true // a short chunk is the last one
	case err != nil:
		return err
	}
	if n < nonceSize+tagSize {
		return fmt.Errorf("file ends in a broken chunk: %w", ErrCorrupt)
	}
	aad := make([]byte, 8, 8+nonceSize)
	binary.BigEndian.PutUint64(aad, r.chunk)
	aad = append(aad, r.nonce...)
	plain, err := r.aead.Open(r.buf[nonceSize:nonceSize], r.buf[:nonceSize], r.buf[nonceSize:n], aad)
	if err != nil {
		return fmt.Errorf("chunk %d failed its check: %w", r.chunk, ErrCorrupt)
	}
	r.chunk++
	r.pending = plain
	return nil
}

func (r *contentReader) Close() error {
	wipe(r.buf)
	return r.f.Close()
}

// Stats counts what DecodeTo wrote.
type Stats struct {
	Files, Dirs, Links int
	Bytes              int64
}

// DecodeTo writes the whole vault as plain files into out, which must not
// exist yet or be empty. Nothing is written outside out, even if the vault
// was tampered with.
func (v *Vault) DecodeTo(out string) (Stats, error) {
	var st Stats
	if entries, err := os.ReadDir(out); err == nil && len(entries) > 0 {
		return st, fmt.Errorf("%s is not empty; choose a new folder", out)
	}
	if err := os.MkdirAll(out, 0700); err != nil {
		return st, err
	}
	root, err := os.OpenRoot(out)
	if err != nil {
		return st, err
	}
	defer root.Close()

	err = v.Walk(func(n Node) error {
		at := filepath.FromSlash(n.Path)
		switch n.Kind {
		case Dir:
			st.Dirs++
			return root.Mkdir(at, 0700)
		case Symlink:
			st.Links++
			return root.Symlink(n.Target, at)
		}
		st.Files++
		src, err := v.Open(n)
		if err != nil {
			return err
		}
		defer src.Close()
		dst, err := root.OpenFile(at, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		written, err := io.Copy(dst, src)
		st.Bytes += written
		if err != nil {
			dst.Close()
			return fmt.Errorf("%q: %w", n.Path, err)
		}
		return dst.Close()
	})
	return st, err
}
