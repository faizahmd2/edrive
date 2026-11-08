package main

import (
	"fmt"
	"os"

	"github.com/faizahmd2/edrive/internal/app"
	"github.com/faizahmd2/edrive/internal/config"
	"github.com/faizahmd2/edrive/internal/help"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		help.PrintAll()
		os.Exit(2)
	}
	command, args := os.Args[1], os.Args[2:]

	if isHelp(command) {
		if len(args) == 1 {
			help.Print(args[0])
		} else {
			help.PrintAll()
		}
		return
	}
	if len(args) > 0 && isHelp(args[len(args)-1]) {
		help.Print(command)
		return
	}

	// Commands that work without any setup.
	switch command {
	case "version", "--version", "-v":
		fmt.Println(version)
		return
	case "decode":
		if len(args) != 2 || !isDir(args[0]) {
			fail(fmt.Errorf("usage: edrive decode <vault-folder> <new-folder>"))
		}
		run(app.DecodeVault(args[0], args[1]))
		return
	case "__clear-clipboard":
		run(app.ClearClipboard(args))
		return
	}

	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		fail(err)
	}
	a := app.App{Config: cfg}

	switch command {
	case "setup":
		run(a.Setup(args))
	case "doctor":
		run(a.Doctor())
	case "open":
		run(a.Open(args))
	case "lock":
		run(a.Lock())
	case "pwd", "cd":
		run(a.Pwd())
	case "status":
		run(a.Status())
	case "pass":
		run(a.Pass(args))
	case "sync", "push", "pull":
		if command == "sync" && len(args) > 0 && args[0] == "mirror" {
			run(a.Mirror(append([]string{"sync"}, args[1:]...)))
		} else {
			run(a.Sync(args))
		}
	case "mirror":
		run(a.Mirror(args))
	case "diff":
		run(a.Diff())
	case "verify":
		run(a.Verify())
	case "cloud":
		run(a.Cloud(args))
	case "remove":
		run(a.Remove())
	case "__sync":
		run(a.BackgroundSync(args))
	case "__guard":
		run(a.RunGuard())
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", command)
		fmt.Fprintln(os.Stderr)
		help.PrintAll()
		os.Exit(2)
	}
}

func run(err error) {
	if err != nil {
		fail(err)
	}
}

func fail(err error) {
	if err.Error() == "cancelled" {
		fmt.Fprintln(os.Stderr, "Cancelled.")
	} else {
		fmt.Fprintln(os.Stderr, "edrive:", err)
	}
	os.Exit(1)
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func isHelp(s string) bool {
	return s == "help" || s == "--help" || s == "-h"
}
