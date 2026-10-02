package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/faiz/edrive/internal/app"
	"github.com/faiz/edrive/internal/config"
	"github.com/faiz/edrive/internal/setup"
)

const version = "0.1.0"

func usage() {
	fmt.Fprintf(os.Stderr, `edrive %s - local-first encrypted developer identity toolkit

	Usage:
	edrive setup
	edrive remove
	edrive purge
	edrive doctor
	edrive status
	edrive unlock
	edrive lock
	edrive backup
	edrive backups
	edrive verify [snapshot] --identity PATH
	edrive restore [snapshot] --identity PATH --output DIR
	edrive identity generate --output PATH
	edrive identity recipient PATH
	edrive identity add PATH --to RECIPIENTS_FILE

	Config:
	EDRIVE_CONFIG   Override config.sh path (default ~/Library/Application Support/edrive/config.sh)

	The live vault remains managed by Cryptomator/FUSE-T. edrive owns the
	independent, portable age-encrypted recovery path and verification.

	`, version)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	command := os.Args[1]
	if command == "version" {
		fmt.Println(version)
		return
	}
	if command == "help" || command == "--help" || command == "-h" {
		fmt.Fprintln(os.Stdout, helpText())
		return
	}

	if command == "setup" || command == "remove" || command == "purge" {
		if len(os.Args) != 2 {
			fmt.Fprintln(os.Stderr, "error:", command, "does not accept arguments")
			os.Exit(2)
		}
		var err error
		switch command {
		case "setup":
			err = setup.Run()
		case "remove":
			err = setup.Remove()
		case "purge":
			err = setup.Purge()
		}
		if err != nil {
			fail(command, err)
		}
		return
	}

	if command == "identity" {
		if err := identityCommand(os.Args[2:]); err != nil {
			fail(command, err)
		}
		return
	}

	configPath := os.Getenv("EDRIVE_CONFIG")
	if configPath == "" {
		configPath = config.DefaultPath()
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		fail(command, err)
	}
	a := app.App{Config: cfg}

	switch command {
	case "doctor":
		err = a.Doctor()
	case "status":
		err = a.Status()
	case "backup":
		err = a.Backup()
	case "backups":
		err = a.Backups()
	case "unlock":
		err = a.Unlock()
	case "lock":
		err = a.Lock()
	case "verify":
		fs := flag.NewFlagSet("verify", flag.ExitOnError)
		identity := fs.String("identity", "", "age identity file")
		_ = fs.Parse(os.Args[2:])
		path := ""
		if fs.NArg() > 0 {
			path = fs.Arg(0)
		}
		err = a.Verify(path, *identity)
	case "restore":
		fs := flag.NewFlagSet("restore", flag.ExitOnError)
		identity := fs.String("identity", "", "age identity file")
		output := fs.String("output", "", "empty directory to restore into")
		_ = fs.Parse(os.Args[2:])
		path := ""
		if fs.NArg() > 0 {
			path = fs.Arg(0)
		}
		err = a.Restore(path, *identity, filepath.Clean(config.Expand(*output)))
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", command)
		usage()
		os.Exit(2)
	}
	if err != nil {
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

func identityCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("identity command required: generate, recipient or add")
	}

	switch args[0] {
	case "generate":
		fs := flag.NewFlagSet("identity generate", flag.ContinueOnError)
		output := fs.String("output", "", "identity output path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return app.GenerateIdentity(config.Expand(*output))

	case "recipient":
		if len(args) < 2 {
			return fmt.Errorf("identity recipient PATH")
		}
		return app.PrintRecipient(config.Expand(args[1]))

	case "add":
		var identityPath string
		var recipientsPath string

		// Accept both:
		//   edrive identity add PATH --to RECIPIENTS
		//   edrive identity add --to RECIPIENTS PATH
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--to":
				if i+1 >= len(args) {
					return fmt.Errorf("identity add: --to requires a recipients file")
				}
				recipientsPath = args[i+1]
				i++
			default:
				if strings.HasPrefix(args[i], "-") {
					return fmt.Errorf("unknown option: %s", args[i])
				}
				if identityPath != "" {
					return fmt.Errorf("identity add: multiple identity paths provided")
				}
				identityPath = args[i]
			}
		}

		if identityPath == "" || recipientsPath == "" {
			return fmt.Errorf("identity add PATH --to RECIPIENTS_FILE")
		}

		return app.AddRecipient(
			config.Expand(identityPath),
			config.Expand(recipientsPath),
		)

	default:
		return fmt.Errorf("unknown identity command: %s", args[0])
	}
}

func helpText() string {
	return fmt.Sprintf(`edrive %s - local-first encrypted developer identity toolkit

Usage:
  edrive setup
  edrive doctor
  edrive status
  edrive unlock
  edrive lock
  edrive backup
  edrive backups
  edrive verify [snapshot] --identity PATH
  edrive restore [snapshot] --identity PATH --output DIR
  edrive identity generate --output PATH
  edrive identity recipient PATH
  edrive identity add PATH --to RECIPIENTS_FILE

Config:
  EDRIVE_CONFIG   Override config.sh path (default ~/Library/Application Support/edrive/config.sh)

The live vault remains managed by Cryptomator/FUSE-T. edrive owns the
independent, portable age-encrypted recovery path and verification.`, version)
}
