package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/term"
)

var input = bufio.NewReader(os.Stdin)

func Open(path string) error {
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
	default:
		return false, nil
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

// ReadSecret reads a line without echoing it. The prompt goes to stderr so
// command substitution like $(edrive ...) stays clean.
func ReadSecret(prompt string) ([]byte, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		line, err := input.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, err
		}
		return []byte(strings.TrimRight(line, "\r\n")), nil
	}
	fmt.Fprint(os.Stderr, prompt)
	secret, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	return secret, err
}

// StdinIsTerminal reports whether input is interactive (not a pipe).
func StdinIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// EditFile opens path in $EDITOR, falling back to nvim, vim or vi.
func EditFile(path string) error {
	candidates := []string{}
	if e := strings.TrimSpace(os.Getenv("EDITOR")); e != "" {
		candidates = append(candidates, e)
	}
	candidates = append(candidates, "nvim", "vim", "vi")
	for _, name := range candidates {
		fields := strings.Fields(name)
		editor, err := exec.LookPath(fields[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(editor, append(fields[1:], filepath.Clean(path))...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	return fmt.Errorf("no terminal editor found; set $EDITOR")
}

func Pause(prompt string) error {
	fmt.Print(prompt)
	_, err := input.ReadString('\n')
	if err == io.EOF {
		return nil
	}
	return err
}

// Wipe overwrites a secret buffer once it is no longer needed.
func Wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
