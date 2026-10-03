package pass

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func Migrate(workspace string) (int, error) {
	dir := filepath.Join(workspace, "pass")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read pass directory: %w", err)
	}

	type item struct {
	from string
	to   string
	name string
}
	var items []item
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".txt") {
			continue
		}
		to := filepath.Join(dir, name+".txt")
		if info, statErr := os.Stat(to); statErr == nil && info.Mode().IsRegular() {
			return 0, fmt.Errorf("cannot migrate pass entry %q: %s already exists", name, filepath.Base(to))
		} else if statErr != nil && !os.IsNotExist(statErr) {
			return 0, fmt.Errorf("check pass entry %q: %w", name, statErr)
		}
		items = append(items, item{
			from: filepath.Join(dir, name),
			to:   to,
			name: name,
		})
	}

	for _, item := range items {
		if err := os.Rename(item.from, item.to); err != nil {
			return 0, fmt.Errorf("migrate pass entry %q: %w", item.name, err)
		}
	}
	return len(items), nil
}
