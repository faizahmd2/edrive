package main

import (
	"fmt"
	"os"

	"github.com/faizahmd2/edrive/internal/app"
	"github.com/faizahmd2/edrive/internal/config"
	"github.com/faizahmd2/edrive/internal/decode"
	"github.com/faizahmd2/edrive/internal/help"
	"github.com/faizahmd2/edrive/internal/setup"
)

const version = "0.4.0"

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(2)
	}

	command := os.Args[1]

	if command == "help" || command == "--help" || command == "-h" {
		if len(os.Args) == 3 && os.Args[2] != "" && !isHelpArg(os.Args[2]) {
			printCommandHelp(os.Args[2])
		} else {
			printHelp()
		}
		return
	}
	if len(os.Args) >= 3 && isHelpArg(os.Args[len(os.Args)-1]) {
		printCommandHelp(command)
		return
	}

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
		case "cloud":
			run(command, func() error {
				return a.Cloud(os.Args[2:])
			})
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
	help.PrintAll()
}

func printCommandHelp(command string) {
	help.Print(command)
}

func isHelpArg(value string) bool {
	return value == "help" || value == "--help" || value == "-h"
}
