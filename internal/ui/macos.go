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

	script := "tell application \\"System Events\\" to activate\n"
	if isDir(defaultDir) {
		script += fmt.Sprintf(
			"set chosenFolder to choose folder with prompt \\"%s\\" default location POSIX file \\"%s\\" \n",
			escapeAppleScript(prompt),
			escapeAppleScript(defaultDir),
		)
	} else {
		script += fmt.Sprintf(
			"set chosenFolder to choose folder with prompt \\"%s\\" \n",
			escapeAppleScript(prompt),
		)
	}
	script += "return POSIX path of chosenFolder"

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
	return "# >>> edrive shell integration >>>\n" +
		"edrive() {\n" +
		"  if [[ \\"$1\\" == \\"cd\\" ]]; then\n" +
		"    shift\n" +
		"    if (( $# > 0 )); then\n" +
		"      echo \\"edrive cd does not accept arguments\\" >&2\n" +
		"      return 2\n" +
		"    fi\n" +
		"    local dir\n" +
		"    dir=(\\"$HOME/.local/bin/edrive\\" cd) || return\n" +
		"    builtin cd -- \\"$dir\\"\n" +
		"    return\n" +
		"  fi\n" +
		"  \\"$HOME/.local/bin/edrive\\" \\"$@\\" \n" +
		"}\n" +
		"# <<< edrive shell integration <<<\n"
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
	return strings.ReplaceAll(value, "\"", "\\\"")
}
