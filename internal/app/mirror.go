package app

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/faizahmd2/edrive/internal/config"
	"github.com/faizahmd2/edrive/internal/mirror"
)

// The mirror copies a folder of sealed secrets into the vault, one way: the
// app on this Mac is the only place a secret changes. See internal/mirror.

// mirrorStaleAfter is when status and doctor start suggesting a copy.
const mirrorStaleAfter = 30 * 24 * time.Hour

// mirrorDir is where the copies live inside the workspace.
func mirrorDir() string { return filepath.Join(config.WorkspacePath(), "mirror") }

// mirrorRecord is kept on this Mac (outside the vault) so status and doctor
// can answer while the vault is locked. It holds no secret.
type mirrorRecord struct {
	At       time.Time `json:"at"`
	Secrets  int       `json:"secrets"`
	Problems int       `json:"problems"`
}

func mirrorRecordPath() string { return filepath.Join(config.StateDir(), "mirror.json") }

func lastMirror() (mirrorRecord, bool) {
	data, err := os.ReadFile(mirrorRecordPath())
	if err != nil {
		return mirrorRecord{}, false
	}
	var r mirrorRecord
	return r, json.Unmarshal(data, &r) == nil
}

func (a App) Mirror(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: edrive mirror init | sync [--allow-deletes]")
	}
	switch args[0] {
	case "init":
		return a.mirrorInit()
	case "sync":
		return a.mirrorSync(args[1:])
	}
	return fmt.Errorf("usage: edrive mirror init | sync [--allow-deletes]")
}

func (a App) mirrorInit() error {
	if err := a.requireSetup(); err != nil {
		return err
	}
	var recipient string
	var created bool
	err := a.withVault("make the mirror key", true, func() error {
		var err error
		recipient, created, err = mirror.Init(mirrorDir())
		return err
	})
	if err != nil {
		return err
	}
	if created {
		fmt.Println("Mirror key made. Its private half stays in the vault (mirror/.sync/key.txt).")
	} else {
		fmt.Println("The mirror key already exists; nothing changed.")
	}
	fmt.Println("Give this public key to the app that seals the secrets:")
	fmt.Println()
	fmt.Println("    " + recipient)
	return nil
}

func (a App) mirrorSync(args []string) error {
	fs := flag.NewFlagSet("mirror sync", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	allowDeletes := fs.Bool("allow-deletes", false, "")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: edrive mirror sync [--allow-deletes]")
	}
	if err := a.requireSetup(); err != nil {
		return err
	}
	sealed := config.Expand(a.Config.MirrorFrom)
	if info, err := os.Stat(sealed); err != nil || !info.IsDir() {
		return fmt.Errorf("no sealed folder at %s yet; the app makes it when it seals its first secret", sealed)
	}

	var plan mirror.Plan
	var res mirror.Result
	var problems []mirror.Problem
	err := a.withVault("copy sealed secrets into the vault", true, func() error {
		dir := mirrorDir()
		key, err := mirror.LoadKey(dir)
		if err != nil {
			return err
		}
		secrets, bad, err := mirror.Read(sealed, key)
		if err != nil {
			return err
		}
		problems = bad
		prev, err := mirror.LoadState(dir)
		if err != nil {
			return err
		}
		plan = mirror.MakePlan(secrets, bad, prev, *allowDeletes)
		problems = append(problems, plan.Clashes...)
		next, r, err := mirror.Apply(dir, plan, prev, time.Now())
		res = r
		if err != nil {
			return err
		}
		return mirror.SaveState(dir, next)
	})
	for _, p := range problems {
		fmt.Printf("  skipped %s: %s\n", p.File, p.Reason)
	}
	if err != nil {
		return err
	}

	data, _ := json.Marshal(mirrorRecord{At: time.Now(), Secrets: res.Total, Problems: len(problems)})
	if err := config.WriteFileAtomic(mirrorRecordPath(), data, 0600); err != nil {
		return err
	}
	for _, p := range res.HandEdited {
		fmt.Printf("  %s was changed in the vault; the Mac's value is back, the edited copy is in mirror/.trash\n", p)
	}
	fmt.Printf("Copied %d new, %d changed, %d removed; %d secret(s) in the vault.\n",
		len(plan.New), res.Written-len(plan.New), res.Removed, res.Total)
	if len(problems) > 0 {
		return fmt.Errorf("%d sealed file(s) skipped; their last copies are unchanged", len(problems))
	}
	return nil
}

// mirrorSummary is the status line, or "" when the mirror was never set up here.
func (a App) mirrorSummary() (line string, stale bool) {
	r, ok := lastMirror()
	if !ok {
		if _, err := os.Stat(config.Expand(a.Config.MirrorFrom)); errors.Is(err, os.ErrNotExist) {
			return "", false
		}
		return "Mirror:    secrets never copied ('edrive mirror sync')", true
	}
	ago := time.Since(r.At)
	line = fmt.Sprintf("Mirror:    %d secret(s) copied %s ago", r.Secrets, roundDuration(ago))
	if r.Problems > 0 {
		line += fmt.Sprintf(", %d skipped", r.Problems)
	}
	return line, ago > mirrorStaleAfter || r.Problems > 0
}
