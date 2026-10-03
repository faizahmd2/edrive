package main

import (
	"fmt"
	"os"

	"github.com/faizahmd2/edrive/internal/app"
	"github.com/faizahmd2/edrive/internal/config"
	"github.com/faizahmd2/edrive/internal/decode"
	"github.com/faizahmd2/edrive/internal/setup"
)

const version = "0.2.0"

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
	case "setup":
		run(command, setup.Run)
	case "doctor":
		cfg, err := config.Load(config.DefaultPath())
		a := app.App{Config: cfg, ConfigError: err}
		run(command, a.Doctor)
	case "remove":
		cfg, err := config.Load(config.DefaultPath())
		a := app.App{Config: cfg, ConfigError: err}
		run(command, a.Remove)
	default:
		cfg, err := config.Load(config.DefaultPath())
		if err != nil {
			fail(command, err)
		}
		a := app.App{Config: cfg}

		switch command {
		case "open":
			run(command, a.Open)
		case "lock":
			run(command, a.Lock)
		case "push":
			run(command, a.Push)
		case "pull":
			run(command, a.Pull)
		case "backup":
			run(command, a.Backup)
		case "pass":
			run(command, func() error {
				return a.Pass(os.Args[2:])
			})
		case "diff":
			run(command, a.Diff)
		case "pwd":
			run(command, a.Pwd)
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
	if command != "doctor" && command != "decode" && command != "setup" && command != "device" && command != "lock" && command != "remove" {
		fmt.Fprintln(os.Stderr, "Run 'edrive doctor' for diagnostics.")
	}
	os.Exit(1)
}

func printHelp() {
	fmt.Printf("edrive %s - local encrypted workspace with explicit cloud sync\n\n", version)
	fmt.Println("Usage:")
	fmt.Println("  edrive setup")
	fmt.Println("  edrive doctor")
	fmt.Println("  edrive open")
	fmt.Println("  edrive lock")
	fmt.Println("  edrive push")
	fmt.Println("  edrive pull")
	fmt.Println("  edrive backup")
	fmt.Println("  edrive pass <name> [value]")
	fmt.Println("  edrive diff")
	fmt.Println("  edrive decode <encrypted-file> <recovery-key>")
	fmt.Println("  edrive remove")
	fmt.Println("  edrive device add <label>")
	fmt.Println("  edrive device list")
	fmt.Println("  edrive device remove <label>")
	fmt.Println()
	fmt.Println("Daily flow:")
	fmt.Println("  edrive open")
	fmt.Println("  # work in ~/.edrive/workspace")
	fmt.Println("  edrive lock")
	fmt.Println("  edrive push")
	fmt.Println()
	fmt.Println("Cloud:")
	fmt.Printf("  rclone remote: %s\n", config.RcloneRemote)
	fmt.Printf("  remote vault:  %s:%s\n", config.RcloneRemote, config.RemoteVault)
	fmt.Println()
	fmt.Println("Recovery:")
	fmt.Println("  edrive backup")
	fmt.Println("  edrive decode <backup.tar.zst.age> <recovery-key.txt>")
}
