package app

import (
	"fmt"

	"github.com/faizahmd2/edrive/internal/cloud"
	"github.com/faizahmd2/edrive/internal/config"
	"github.com/faizahmd2/edrive/internal/ui"
)

func (a App) Cloud(args []string) error {
	switch len(args) {
	case 0:
		return fmt.Errorf("usage: edrive cloud add [provider] | edrive cloud remove")
	case 1:
		switch args[0] {
		case "add":
			return a.cloudAdd("")
		case "remove":
			return a.cloudRemove()
		default:
			return fmt.Errorf("unknown cloud command %q; use 'edrive cloud help'", args[0])
		}
	case 2:
		if args[0] != "add" {
			return fmt.Errorf("usage: edrive cloud add [provider] | edrive cloud remove")
		}
		return a.cloudAdd(args[1])
	default:
		return fmt.Errorf("usage: edrive cloud add [provider] | edrive cloud remove")
	}
}

func (a App) cloudAdd(providerInput string) error {
	rc, err := a.rclone()
	if err != nil {
		return err
	}
	if rc.RemoteExists() {
		return fmt.Errorf("cloud remote %q is already configured; run 'edrive cloud remove' first", config.RcloneRemote)
	}

	var provider cloud.Provider
	if providerInput == "" {
		provider, err = cloud.PromptProvider()
	} else {
		provider, err = cloud.ProviderFromString(providerInput)
	}
	if err != nil {
		return err
	}

	if err := cloud.Setup(rc, provider); err != nil {
		return err
	}
	if err := rc.EnsureRemoteDir(); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("Cloud provider is ready.")
	fmt.Println("Run 'edrive sync' to upload your vault.")
	return nil
}

func (a App) cloudRemove() error {
	rc, err := a.rclone()
	if err != nil {
		return err
	}
	if !rc.RemoteExists() {
		fmt.Printf("Cloud remote %q is already not configured.\n", config.RcloneRemote)
		return nil
	}

	ok, err := ui.Confirm("Remove the configured cloud provider from edrive? This does not delete cloud data or the local vault.")
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("Cancelled.")
		return nil
	}

	if err := rc.DeleteRemote(); err != nil {
		return err
	}
	fmt.Println("Cloud provider configuration removed.")
	fmt.Println("Cloud data was not deleted.")
	return nil
}
