package app

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCompareTrees(t *testing.T) {
	decoded := map[string]string{"a": "1", "b": "2", "dir": "dir", "only-decoded": "x"}
	shown := map[string]string{"a": "1", "b": "changed", "dir": "dir", "only-shown": "y"}
	want := []string{"differs: b", "missing from the decoder: only-shown", "only in the decoder: only-decoded"}
	if got := compareTrees(decoded, shown); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	if got := compareTrees(decoded, decoded); len(got) != 0 {
		t.Fatalf("a tree must equal itself, got %v", got)
	}
}

// The workspace side leaves out macOS "._" companions, the same as the decoder side.
func TestHashTreeSkipsAppleDouble(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "f"), 0700)
	_ = os.WriteFile(filepath.Join(root, "f", "note.txt"), []byte("hi"), 0600)
	_ = os.WriteFile(filepath.Join(root, "f", "._note.txt"), []byte("xattr"), 0600)
	_ = os.Symlink("note.txt", filepath.Join(root, "f", "link"))
	tree, err := hashTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != 3 || tree["f"] != "dir" || tree["f/link"] != "link:note.txt" || tree["f/note.txt"] == "" {
		t.Fatalf("got %v", tree)
	}
}
