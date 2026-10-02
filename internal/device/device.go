package device

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/faiz/edrive/internal/ageutil"
	"github.com/faiz/edrive/internal/config"
	"github.com/faiz/edrive/internal/keychain"
)

var labelPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

type Record struct {
	Label     string    `json:"label"`
	Recipient string    `json:"recipient"`
	CreatedAt time.Time `json:"created_at"`
}

type Registry struct {
	Version int      `json:"version"`
	Devices []Record `json:"devices"`
}

func Load() (Registry, error) {
	path := config.DevicesPath()
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Registry{Version: 1}, nil
		}
		return Registry{}, fmt.Errorf("open device registry: %w", err)
	}
	defer f.Close()

	var reg Registry
	if err := json.NewDecoder(f).Decode(&reg); err != nil {
		return Registry{}, fmt.Errorf("decode device registry: %w", err)
	}
	if reg.Version == 0 {
		reg.Version = 1
	}
	if reg.Version != 1 {
		return Registry{}, fmt.Errorf("unsupported device registry version: %d", reg.Version)
	}
	sort.Slice(reg.Devices, func(i, j int) bool {
		return reg.Devices[i].Label < reg.Devices[j].Label
	})
	return reg, nil
}

func (r Registry) Save() error {
	if r.Version == 0 {
		r.Version = 1
	}
	sort.Slice(r.Devices, func(i, j int) bool {
		return r.Devices[i].Label < r.Devices[j].Label
	})

	if err := os.MkdirAll(config.Home(), 0700); err != nil {
		return fmt.Errorf("create device state: %w", err)
	}
	f, err := os.CreateTemp(config.Home(), ".devices-*.tmp")
	if err != nil {
		return fmt.Errorf("create device registry temp: %w", err)
	}
	path := f.Name()
	defer func() {
		_ = f.Close()
		_ = os.Remove(path)
	}()

	if err := f.Chmod(0600); err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(path, config.DevicesPath())
}

func Import(label, identity, ageKeygen string) (Record, error) {
	if !labelPattern.MatchString(label) {
		return Record{}, fmt.Errorf("invalid device label %q", label)
	}
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return Record{}, fmt.Errorf("device identity is empty")
	}

	reg, err := Load()
	if err != nil {
		return Record{}, err
	}
	for _, d := range reg.Devices {
		if d.Label == label {
			return Record{}, fmt.Errorf("device %q already exists", label)
		}
	}

	recipient, err := ageutil.Recipient(identity, ageKeygen, config.TempDir())
	if err != nil {
		return Record{}, err
	}
	if err := keychain.SetIdentity(label, identity); err != nil {
		return Record{}, err
	}

	rec := Record{
		Label:     label,
		Recipient: recipient,
		CreatedAt: time.Now().UTC(),
	}
	reg.Devices = append(reg.Devices, rec)
	if err := reg.Save(); err != nil {
		_ = keychain.DeleteIdentity(label)
		return Record{}, err
	}
	return rec, nil
}

func AddGenerated(label, ageKeygen string) (Record, error) {
	if !labelPattern.MatchString(label) {
		return Record{}, fmt.Errorf("invalid device label %q", label)
	}

	reg, err := Load()
	if err != nil {
		return Record{}, err
	}
	for _, d := range reg.Devices {
		if d.Label == label {
			return Record{}, fmt.Errorf("device %q already exists", label)
		}
	}

	identity, err := ageutil.GenerateIdentity(ageKeygen, config.TempDir())
	if err != nil {
		return Record{}, err
	}
	return Import(label, identity, ageKeygen)
}

func Ensure(label, ageKeygen string) (Record, error) {
	reg, err := Load()
	if err != nil {
		return Record{}, err
	}
	for _, d := range reg.Devices {
		if d.Label == label {
			if !keychain.IdentityExists(label) {
				return Record{}, fmt.Errorf("device %q is missing from Keychain", label)
			}
			return d, nil
		}
	}

	if keychain.IdentityExists(label) {
		identity, err := keychain.GetIdentity(label)
		if err != nil {
			return Record{}, fmt.Errorf("device Keychain access was not granted")
		}
		return Import(label, identity, ageKeygen)
	}
	return AddGenerated(label, ageKeygen)
}

func Remove(label string) error {
	reg, err := Load()
	if err != nil {
		return err
	}

	found := false
	keep := make([]Record, 0, len(reg.Devices))
	var identity string
	for _, d := range reg.Devices {
		if d.Label == label {
			found = true
			continue
		}
		keep = append(keep, d)
	}
	if !found {
		return fmt.Errorf("device %q does not exist", label)
	}
	if len(keep) == 0 {
		return fmt.Errorf("cannot remove the last device identity")
	}

	if keychain.IdentityExists(label) {
		identity, err = keychain.GetIdentity(label)
		if err != nil {
			return fmt.Errorf("edrive needs access to device %q before removing it", label)
		}
		if err := keychain.DeleteIdentity(label); err != nil {
			return err
		}
	}

	original := reg.Devices
	reg.Devices = keep
	if err := reg.Save(); err != nil {
		if identity != "" {
			_ = keychain.SetIdentity(label, identity)
		}
		reg.Devices = original
		return err
	}
	return nil
}

func List() ([]Record, error) {
	reg, err := Load()
	if err != nil {
		return nil, err
	}
	return reg.Devices, nil
}

func Recipients() ([]string, error) {
	devices, err := List()
	if err != nil {
		return nil, err
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("no device identities configured")
	}

	recipients := make([]string, 0, len(devices))
	for _, d := range devices {
		if strings.TrimSpace(d.Recipient) == "" {
			return nil, fmt.Errorf("device %q has no recipient", d.Label)
		}
		recipients = append(recipients, d.Recipient)
	}
	return recipients, nil
}
