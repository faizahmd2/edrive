package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Object struct {
	Name    string
	Size    int64
	ModTime time.Time
}

// Provider is the only storage contract edrive needs. Implementations may map
// this to a local filesystem, Google Drive, S3, R2, SFTP, etc. The caller only
// supplies/consumes byte streams and object names.
type Provider interface {
	Write(name string, fn func(io.Writer) error) error
	Open(name string) (io.ReadCloser, error)
	List(suffix string) ([]Object, error)
	Remove(name string) error
}

type Local struct {
	Root string
}

func NewLocal(root string) (*Local, error) {
	if root == "" {
		return nil, fmt.Errorf("local storage root is required")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	return &Local{Root: root}, nil
}

func (p *Local) Write(name string, fn func(io.Writer) error) error {
	name, err := cleanObjectName(name)
	if err != nil {
		return err
	}
	tmp := filepath.Join(p.Root, "."+filepath.Base(name)+fmt.Sprintf(".tmp-%d", os.Getpid()))
	final := filepath.Join(p.Root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(final), 0700); err != nil {
		return err
	}
	_ = os.Remove(tmp)

	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(tmp)
		}
	}()

	if err := fn(f); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync snapshot: %w", err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		return fmt.Errorf("finalize object: %w", err)
	}
	ok = true
	return nil
}

func (p *Local) Open(name string) (io.ReadCloser, error) {
	name, err := cleanObjectName(name)
	if err != nil {
		return nil, err
	}
	return os.Open(filepath.Join(p.Root, filepath.FromSlash(name)))
}

func (p *Local) List(suffix string) ([]Object, error) {
	entries, err := os.ReadDir(p.Root)
	if err != nil {
		return nil, err
	}
	objects := make([]Object, 0)
	for _, e := range entries {
		if e.IsDir() || (suffix != "" && !strings.HasSuffix(e.Name(), suffix)) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		objects = append(objects, Object{Name: e.Name(), Size: info.Size(), ModTime: info.ModTime()})
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].ModTime.After(objects[j].ModTime) })
	return objects, nil
}

func (p *Local) Remove(name string) error {
	name, err := cleanObjectName(name)
	if err != nil {
		return err
	}
	return os.Remove(filepath.Join(p.Root, filepath.FromSlash(name)))
}

func cleanObjectName(name string) (string, error) {
	name = filepath.ToSlash(name)
	clean := filepath.ToSlash(filepath.Clean(name))
	if name == "" || clean == "." || strings.HasPrefix(clean, "/") || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("unsafe storage object name: %q", name)
	}
	return clean, nil
}
