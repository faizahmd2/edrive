package app

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/faizahmd2/edrive/internal/config"
	"github.com/faizahmd2/edrive/internal/macos"
	"github.com/faizahmd2/edrive/internal/pass"
	"github.com/faizahmd2/edrive/internal/ui"
)

const clipboardClearAfter = 30 * time.Second

const passUsage = `usage:
  edrive pass <key>          print a secret
  edrive pass <key> -c       copy it (clipboard clears after 30s)
  edrive pass ls             list keys
  edrive pass set <key>      save a secret (typed hidden, or piped on stdin)`

func (a App) Pass(args []string) error {
	copyFlag := false
	var rest []string
	for _, arg := range args {
		if arg == "-c" || arg == "--copy" {
			copyFlag = true
			continue
		}
		rest = append(rest, arg)
	}

	switch {
	case len(rest) == 1 && (rest[0] == "ls" || rest[0] == "list"):
		return a.withVault("list your edrive secrets", false, func() error {
			names, err := pass.List(config.WorkspacePath())
			if err != nil {
				return err
			}
			for _, name := range names {
				fmt.Println(name)
			}
			return nil
		})

	case len(rest) == 2 && rest[0] == "set":
		return a.passSet(rest[1])

	case len(rest) == 1:
		var value string
		err := a.withVault("show an edrive secret", false, func() error {
			v, err := pass.Get(config.WorkspacePath(), rest[0])
			value = v
			return err
		})
		if err != nil {
			return err
		}
		if copyFlag {
			count := macos.ClipboardSet(value)
			_ = spawnDetached("__clear-clipboard", strconv.FormatInt(count, 10))
			fmt.Fprintf(os.Stderr, "Copied %s to clipboard (clears in %ds).\n", rest[0], int(clipboardClearAfter.Seconds()))
			return nil
		}
		fmt.Println(value)
		return nil

	case len(rest) == 2:
		return fmt.Errorf("to keep secrets out of your shell history, use: edrive pass set %s", rest[0])

	default:
		return fmt.Errorf("%s", passUsage)
	}
}

func (a App) passSet(name string) error {
	if err := pass.ValidateName(name); err != nil {
		return err
	}
	var value []byte
	var err error
	if ui.StdinIsTerminal() {
		value, err = ui.ReadSecret(fmt.Sprintf("Value for %s (hidden): ", name))
	} else {
		value, err = io.ReadAll(os.Stdin) // multi-line values: pbpaste | edrive pass set key
	}
	if err != nil {
		return err
	}
	defer ui.Wipe(value)
	text := string(value)
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("value is empty; nothing saved")
	}
	err = a.withVault("save an edrive secret", true, func() error {
		return pass.SetText(config.WorkspacePath(), name, text)
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Saved %s.\n", name)
	return nil
}

// withVault runs fn with the workspace mounted. If it had to unlock, it locks
// again straight away (and syncs if fn changed something).
func (a App) withVault(reason string, writes bool, fn func() error) error {
	if mounted() {
		return fn()
	}
	return withLock(func() error {
		opened, err := a.unlock(reason)
		if err != nil {
			return err
		}
		fnErr := fn()
		if opened {
			if err := a.crypto().Lock(); err != nil && fnErr == nil {
				fnErr = err
			}
			if writes {
				startBackgroundSync(syncAfterLock)
			}
		}
		return fnErr
	})
}

// ClearClipboard is the hidden helper behind 'pass -c'.
func ClearClipboard(args []string) error {
	if len(args) != 1 {
		return nil
	}
	count, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return nil
	}
	time.Sleep(clipboardClearAfter)
	macos.ClipboardClearIf(count)
	return nil
}
