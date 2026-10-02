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

func ChooseOption(prompt string, options []string, defaultIndex int) (string, bool, error) {
	if len(options) == 0 {
		return "", false, fmt.Errorf("no options were provided")
	}
	if defaultIndex < 0 || defaultIndex >= len(options) {
		return "", false, fmt.Errorf("invalid default option")
	}

	items := make([]string, 0, len(options))
	for _, option := range options {
		items = append(items, fmt.Sprintf(`"%s"`, escapeAppleScript(option)))
	}

	script := fmt.Sprintf(`tell application "System Events" to activate

set choices to {%s}
set selectedItems to choose from list choices with prompt "%s" default items {"%s"}
if selectedItems is false then return ""
return item 1 of selectedItems`,
		strings.Join(items, ", "),
		escapeAppleScript(prompt),
		escapeAppleScript(options[defaultIndex]),
	)

	out, err := exec.Command("osascript", "-e", script).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "User canceled") || strings.Contains(string(out), "(-128)") {
			return "", false, nil
		}
		return "", false, fmt.Errorf("choose option: %w", err)
	}
	selected := strings.TrimSpace(string(out))
	if selected == "" {
		return "", false, nil
	}
	return selected, true, nil
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
# <<< edrive shell integration <<<` + "\n"
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
