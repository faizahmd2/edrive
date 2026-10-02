package device

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/faiz/edrive/internal/config"
	"github.com/faiz/edrive/internal/keychain"
	"github.com/faiz/edrive/internal/ageutil"
)

var labelPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}package device

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/faiz/edrive/internal/config"
	"github.com/faiz/edrive/internal/keychain"
)

var labelPattern = )

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
	return AddGenerated(label, ageKeygen)
}

func Remove(label string) error {
	reg, err := Load()
	if err != nil {
		return err
	}
	found := false
	devices := reg.Devices[:0]
	for _, d := range reg.Devices {
		if d.Label == label {
			found = true
			continue
		}
		devices = append(devices, d)
	}
	if !found {
		return fmt.Errorf("device %q does not exist", label)
	}
	if len(devices) == 0 {
		return fmt.Errorf("cannot remove the last device identity")
	}
	if err := keychain.DeleteIdentity(label); err != nil {
		return err
	}
	reg.Devices = devices
	return reg.Save()
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
