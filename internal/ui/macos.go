package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func ReadLine(prompt string) (string, error) {
	fmt.Print(prompt)
	line, err := input.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func ReadSecret(prompt string) (string, error) {
	fmt.Print(prompt)
	if err := exec.Command("stty", "-echo").Run(); err != nil {
		return ReadLine("")
	}
	defer func() {
		_ = exec.Command("stty", "echo").Run()
		fmt.Println()
	}()

	line, err := input.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func EditTextFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("path is required")
	}
	if runtime.GOOS == "darwin" {
		path = filepath.Clean(path)

		// Start an isolated TextEdit instance with editing substitutions disabled.
		// These are command-line defaults, so they affect only this TextEdit launch.
		args := []string{
			"/usr/bin/open", "-na", "TextEdit", "--args",
			"-RichText", "0",
			"-TextReplacement", "0",
			"-NSAutomaticCapitalizationEnabled", "NO",
			"-NSAutomaticDashSubstitutionEnabled", "NO",
			"-NSAutomaticPeriodSubstitutionEnabled", "NO",
			"-NSAutomaticQuoteSubstitutionEnabled", "NO",
			"-NSAutomaticSpellingCorrectionEnabled", "NO",
			"-NSAutomaticTextCompletionEnabled", "NO",
			"-NSAutomaticTextReplacementEnabled", "NO",
			"-NSAutomaticDataDetectionEnabled", "NO",
			"-NSAutomaticInlinePredictionEnabled", "NO",
		}
		if err := exec.Command(args[0], args[1:]...).Run(); err == nil {
			return waitForTextEditDocument(path)
		}
	}
	return EditFile(path)
}

func waitForTextEditDocument(path string) error {
	name := filepath.Base(path)
	script := fmt.Sprintf(`set targetName to %q
tell application "TextEdit"
	activate
	open POSIX file %q

	repeat
		try
			set targetOpen to false
			repeat with d in documents
				if name of d is targetName then
					set targetOpen to true
					exit repeat
				end if
			end repeat
			on error
			set targetOpen to false
		end try

		if targetOpen is false then exit repeat
		delay 0.25
	end repeat

	try
		quit
	end try
end tell`, name, path)

	if err := exec.Command("/usr/bin/osascript", "-e", script).Run(); err != nil {
		return fmt.Errorf("wait for TextEdit: %w", err)
	}
	return nil
}

func EditFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("path is required")
	}
	for _, name := range []string{"nvim", "vim", "vi"} {
		editor, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		cmd := exec.Command(editor, filepath.Clean(path))
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	return fmt.Errorf("no terminal editor found; install neovim/vim or use the macOS-provided vi")
}

func Pause(prompt string) error {
	fmt.Print(prompt)
	_, err := input.ReadString('\n')
	if err == io.EOF {
		return nil
	}
	return err
}
