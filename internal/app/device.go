package app

import (
	"fmt"
	"strings"

	"github.com/faiz/edrive/internal/ageutil"
	"github.com/faiz/edrive/internal/device"
	"github.com/faiz/edrive/internal/ui"
)

func (a App) Device(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("use: edrive device add <label> | list | remove <label>")
	}

	switch args[0] {
	case "add":
		if len(args) != 2 {
			return fmt.Errorf("use: edrive device add <label>")
		}
		return a.addDevice(args[1])
	case "list":
		if len(args) != 1 {
			return fmt.Errorf("use: edrive device list")
		}
		return a.listDevices()
	case "remove":
		if len(args) != 2 {
			return fmt.Errorf("use: edrive device remove <label>")
		}
		return a.removeDevice(args[1])
	default:
		return fmt.Errorf("unknown device command %q", args[0])
	}
}

func (a App) addDevice(label string) error {
	if !a.Config.ConfigFound {
		return fmt.Errorf("edrive is not set up; run 'edrive setup' first")
	}
	if strings.TrimSpace(a.Config.AgePath) == "" {
		return fmt.Errorf("edrive age tool is not configured; run 'edrive setup' first")
	}
	keygenPath, err := ageutil.KeygenPath(a.Config.AgePath)
	if err != nil {
		return err
	}
	rec, err := device.AddGenerated(strings.TrimSpace(label), keygenPath)
	if err != nil {
		return err
	}
	fmt.Printf("Device added: %s\n", rec.Label)
	return nil
}

func (a App) listDevices() error {
	devices, err := device.List()
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		fmt.Println("No device identities.")
		return nil
	}
	for _, d := range devices {
		fmt.Println(d.Label)
	}
	return nil
}

func (a App) removeDevice(label string) error {
	label = strings.TrimSpace(label)
	if label == "" {
		return fmt.Errorf("device label is required")
	}
	ok, err := ui.Confirm("Remove device identity " + label + "? This device will not be included in future backups.")
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("Cancelled.")
		return nil
	}
	if err := device.Remove(label); err != nil {
		return err
	}
	fmt.Printf("Device removed: %s\n", label)
	return nil
}
