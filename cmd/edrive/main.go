package main

import (
	"fmt"
	"os"

	"github.com/faiz/edrive/internal/app"
	"github.com/faiz/edrive/internal/config"
	"github.com/faiz/edrive/internal/setup"
	"github.com/faiz/edrive/internal/ui"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(2)
	}

	command := os.Args[1]
	switch command {
	case "version":
		fmt.Println(version)
		return
	case "help", "--help", "-h":
		printHelp()
		return
	case "shell-init":
		if len(os.Args) != 3 || os.Args[2] != "zsh" {
			fmt.Fprintln(os.Stderr, "error: shell-init zsh is the supported shell integration")
			os.Exit(2)
		}
		fmt.Print(ui.ShellInitZsh())
		return
	case "setup":
		run(command, setup.Run)
		return
	case "remove":
		run(command, setup.Remove)
		return
	case "purge":
		run(command, setup.Purge)
		return
	}

	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		fail(command, err)
	}
	a := app.App{Config: cfg}

	switch command {
	case "doctor":
		run(command, a.Doctor)
	case "status":
		run(command, a.Status)
	case "open":
		run(command, a.Open)
	case "cd":
		run(command, a.CD)
	case "close", "lock":
		run(command, a.Close)
	case "unlock":
		run(command, a.Unlock)
	case "backup":
		run(command, a.Backup)
	case "restore":
		run(command, a.Restore)
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", command)
		printHelp()
		os.Exit(2)
	}
}

func run(command string, fn func() error) {
	if err := fn(); err != nil {
		fail(command, err)
	}
}

func fail(command string, err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	if command != "doctor" {
		fmt.Fprintln(os.Stderr, "Run 'edrive doctor' for diagnostics.")
	}
	os.Exit(1)
}

func printHelp() {
	fmt.Printf(`edrive %s - local-first encrypted workspace orchestrator

Usage:
  edrive setup
  edrive open
  edrive cd
  edrive close
  edrive backup
  edrive restore
  edrive status
  edrive doctor
  edrive remove
  edrive purge

Other:
  edrive version

The working files live in the mounted workspace you choose during setup.
Google Drive and Cryptomator remain external providers. edrive only
orchestrates them locally.

Backup creates one encrypted recovery package and lets you choose where it
is saved. The previous backup destination is remembered for the next backup.

Normal command errors are intentionally short. Run:
  edrive doctor
`, version)
}
