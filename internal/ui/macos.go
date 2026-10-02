package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var input = bufio.NewReader(os.Stdin)

func Open(path string) error {
	if path == "" {
		return fmt.Errorf("path is required")
	}
	return exec.Command("/usr/bin/open", filepath.Clean(path)).Run()
}

func OpenApplication(name string) error {
	return exec.Command("/usr/bin/open", "-a", name).Run()
}

func ChooseFolder(prompt, defaultDir string) (string, bool, error) {
	defaultDir = filepath.Clean(defaultDir)

	var script string
	if isDir(defaultDir) {
		script = fmt.Sprintf(`tell application "System Events" to activate

set chosenFolder to choose folder with prompt "%s" default location POSIX file "%s"
return POSIX path of chosenFolder`, escapeAppleScript(prompt), escapeAppleScript(defaultDir))
	} else {
		script = fmt.Sprintf(`tell application "System Events" to activate

set chosenFolder to choose folder with prompt "%s"
return POSIX path of chosenFolder`, escapeAppleScript(prompt))
	}

	out, err := exec.Command("osascript", "-e", script).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "User canceled") || strings.Contains(string(out), "(-128)") {
			return "", false, nil
		}
		return "", false, fmt.Errorf("choose folder: %w", err)
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", false, fmt.Errorf("no folder selected")
	}
	return filepath.Clean(path), true, nil
}

func ShellInitZsh() string {
	return `# >>> edrive shell integration >>>
edrive() {
  if [[ "$1" == "cd" ]]; then
    shift
    if (( $# > 0 )); then
      echo "edrive cd does not accept arguments" >&2
      return 2
    fi
    local dir
    dir=$("$HOME/.local/bin/edrive" cd) || return
    builtin cd -- "$dir"
    return
  fi
  "$HOME/.local/bin/edrive" "$@"
}
# <<< edrive shell integration <<<` + "
"
}

func Confirm(prompt string) (bool, error) {
	fmt.Print(prompt + " [y/N] ")
	line, err := input.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	case "", "n", "no":
		return false, nil
	default:
		return false, fmt.Errorf("please answer yes or no")
	}
}

func Pause(prompt string) error {
	fmt.Print(prompt)
	_, err := input.ReadString('\n')
	if err == io.EOF {
		return nil
	}
	return err
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func escapeAppleScript(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	return strings.ReplaceAll(value, `"`, `\"`)
}
