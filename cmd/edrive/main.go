package main

import (
	"fmt"
	"os"

	"github.com/faizahmd2/edrive/internal/app"
	"github.com/faizahmd2/edrive/internal/config"
	"github.com/faizahmd2/edrive/internal/decode"
	"github.com/faizahmd2/edrive/internal/setup"
	"github.com/faizahmd2/edrive/internal/ui"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(2)
	}

	command := os.Args[1]

	switch command {
	case "help", "--help", "-h":
		printHelp()
	case "version":
		fmt.Println(version)
	case "decode":
		if len(os.Args) != 4 {
			fail(command, fmt.Errorf("usage: edrive decode <encrypted-file> <recovery-key>"))
		}
		output, err := decode.File(os.Args[2], os.Args[3])
		if err != nil {
			fail(command, err)
		}
		fmt.Println("Recovered:", output)
	case "shell-init":
		if len(os.Args) != 3 || os.Args[2] != "zsh" {
			fail(command, fmt.Errorf("usage: edrive shell-init zsh"))
		}
		fmt.Print(ui.ShellInitZsh())
	case "setup":
		run(command, setup.Run)
	case "doctor":
		cfg, err := config.Load(config.DefaultPath())
		a := app.App{Config: cfg, ConfigError: err}
		run(command, a.Doctor)
	default:
		cfg, err := config.Load(config.DefaultPath())
		if err != nil {
			fail(command, err)
		}
		a := app.App{Config: cfg}

		switch command {
		case "open":
			run(command, a.Open)
		case "cd":
			run(command, a.CD)
		case "unlock":
			run(command, a.Unlock)
		case "lock":
			run(command, a.Lock)
		case "backup":
			run(command, a.Backup)
		case "device":
			run(command, func() error {
				return a.Device(os.Args[2:])
			})
		default:
			fmt.Fprintln(os.Stderr, "unknown command:", command)
			printHelp()
			os.Exit(2)
		}
	}
}

func run(command string, fn func() error) {
	if err := fn(); err != nil {
		fail(command, err)
	}
}

func fail(command string, err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	if command != "doctor" && command != "decode" && command != "setup" && command != "device" && command != "lock" {
		fmt.Fprintln(os.Stderr, "Run 'edrive doctor' for diagnostics.")
	}
	os.Exit(1)
}

func printHelp() {
	fmt.Printf(`edrive %s - local-first encrypted workspace

Usage:
  edrive setup
  edrive doctor
  edrive open
  edrive cd
  edrive unlock
  edrive lock
  edrive backup
  edrive decode <encrypted-file> <recovery-key>
  edrive device add <label>
  edrive device list
  edrive device remove <label>

Other:
  edrive help
  edrive version

Each fresh setup asks for the workspace location again.
Google Drive and Cryptomator remain external; edrive discovers and uses only the resources explicitly selected for it.

Backup:
  edrive backup

Backup creates one encrypted backup file in a folder you choose.
The destination is remembered and opened in Finder after backup.

Recovery:
  The recovery key is created once on the first backup and kept in the
  macOS Keychain. It is not printed by edrive.

Decode:
  edrive decode backup.tar.zst.age recovery-key.txt

Decode decrypts, decompresses, and extracts the backup into a new sibling
folder. It requires both age and zstd, but does not require edrive setup.
`, version)
}
