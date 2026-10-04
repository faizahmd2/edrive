package cloud

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/faizahmd2/edrive/internal/rclone"
	"github.com/faizahmd2/edrive/internal/ui"
)

const remoteName = "edrive-cloud"

type Provider struct {
	Code       string
	Name       string
	RcloneType string
	Backend    string
	OAuth      bool
}

var providers = []Provider{
	{Code: "1", Name: "Google Drive", RcloneType: "drive", OAuth: true},
	{Code: "2", Name: "Amazon S3 / S3-compatible", RcloneType: "s3"},
	{Code: "e", Name: "Cloudflare R2", RcloneType: "s3", Backend: "Cloudflare"},
	{Code: "3", Name: "Backblaze B2", RcloneType: "b2"},
	{Code: "4", Name: "Dropbox", RcloneType: "dropbox", OAuth: true},
	{Code: "5", Name: "Microsoft OneDrive", RcloneType: "onedrive", OAuth: true},
}

func EnsureConfigured(rc *rclone.Client) error {
	if rc.RemoteExists() {
		if err := rc.CheckConfigured(); err == nil {
			return nil
		} else if remoteType, _, identityErr := rc.RemoteIdentity(); identityErr == nil && remoteType == "drive" {
			return fmt.Errorf("%w; Google Drive authorization may have expired, run 'edrive cloud remove' then 'edrive cloud add google' to reconfigure", err)
		}
		return rc.EnsureConfigured()
	}

	fmt.Println()
	fmt.Println("No cloud provider is configured for edrive.")
	fmt.Println("Choose how to configure the fixed edrive-cloud remote:")
	fmt.Println("  1) Built-in rclone setup")
	fmt.Println("  2) edrive guided setup")

	method, err := ui.ReadLine("Choose setup method [1]: ")
	if err != nil {
		return err
	}
	if method == "" {
		method = "1"
	}

	switch method {
	case "1":
		return rc.EnsureConfigured()
	case "2":
		provider, err := PromptProvider()
		if err != nil {
			return err
		}
		return Setup(rc, provider)
	default:
		return fmt.Errorf("unknown setup method %q; choose 1 or 2", method)
	}
}

func ProviderMenu() {
	fmt.Println("Cloud providers:")
	fmt.Println("  1) Google Drive")
	fmt.Println("  2) Amazon S3 / S3-compatible")
	fmt.Println("  e) Cloudflare R2")
	fmt.Println("  3) Backblaze B2")
	fmt.Println("  4) Dropbox")
	fmt.Println("  5) Microsoft OneDrive")
}

func PromptProvider() (Provider, error) {
	ProviderMenu()
	value, err := ui.ReadLine("Choose provider [1]: ")
	if err != nil {
		return Provider{}, err
	}
	if value == "" {
		value = "1"
	}
	return providerFor(value)
}

func providerFor(value string) (Provider, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	aliases := map[string]string{
		"google": "1", "drive": "1", "gdrive": "1",
		"aws": "2", "s3": "2",
		"r2": "e", "cloudflare": "e", "cloudflare-r2": "e",
		"b2": "3", "backblaze": "3",
		"dropbox":  "4",
		"onedrive": "5", "one-drive": "5",
	}
	if alias, ok := aliases[value]; ok {
		value = alias
	}
	for _, p := range providers {
		if p.Code == value {
			return p, nil
		}
	}
	return Provider{}, fmt.Errorf("unknown cloud provider %q", value)
}

func ProviderFromString(value string) (Provider, error) {
	return providerFor(value)
}

func Setup(rc *rclone.Client, provider Provider) error {
	if rc.RemoteExists() {
		return fmt.Errorf("cloud remote %q is already configured; run 'edrive cloud remove' first", remoteName)
	}

	switch provider.Code {
	case "1":
		return setupGoogle(rc, provider)
	case "2":
		return setupS3(rc, provider)
	case "e":
		return setupR2(rc, provider)
	case "3":
		return setupB2(rc, provider)
	case "4":
		return setupOAuth(rc, provider, "Dropbox")
	case "5":
		return setupOAuth(rc, provider, "Microsoft OneDrive")
	default:
		return fmt.Errorf("unsupported cloud provider %q", provider.Name)
	}
}

func setupGoogle(rc *rclone.Client, provider Provider) error {
	fmt.Println()
	fmt.Println("Google Drive uses OAuth.")
	fmt.Println("Press Enter for both values to use rclone's shared/public client.")
	fmt.Println("Rclone's current documentation says its shared Google client is being retired during 2026; using your own client avoids that dependency.")

	clientID, err := ui.ReadLine("Google client ID [shared]: ")
	if err != nil {
		return err
	}
	secret, err := ui.ReadSecret("Google client secret [shared]: ")
	if err != nil {
		return err
	}
	clientSecret := strings.TrimSpace(string(secret))
	if (clientID == "") != (clientSecret == "") {
		return fmt.Errorf("Google client ID and client secret must both be provided, or both left empty")
	}
	fmt.Println("Note: Google OAuth apps left in Testing can have grants expire after 7 days; reconfigure the cloud provider when that happens.")

	configPath, err := rc.ConfigFile()
	if err != nil {
		return err
	}
	if err := validateExistingConfig(configPath); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("Opening Google authorization in your browser...")
	var credentials []string
	if clientID != "" {
		credentials = []string{clientID, clientSecret}
	}
	token, err := rc.Authorize(provider.RcloneType, credentials...)
	if err != nil {
		return err
	}

	options := map[string]string{
		"scope":           "drive",
		"config_is_local": "true",
	}
	if clientID != "" {
		options["client_id"] = clientID
		options["client_secret"] = clientSecret
	}
	if err := rc.CreateRemote(provider.RcloneType, options); err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = rc.DeleteRemote()
		}
	}()

	if err := validateEditableConfig(configPath); err != nil {
		return err
	}
	if err := PatchValue(configPath, remoteName, "token", token); err != nil {
		return fmt.Errorf("save Google OAuth token: %w", err)
	}
	if err := rc.CheckConfigured(); err != nil {
		return err
	}

	cleanup = false
	fmt.Println()
	fmt.Println("Cloud provider configured:", provider.Name)
	fmt.Println("Rclone config saved:", configPath)
	return nil
}

func setupS3(rc *rclone.Client, provider Provider) error {
	return createEditAuthenticate(rc, provider, map[string]string{
		"provider": "AWS",
		"env_auth": "false",
	}, nil)
}

func setupR2(rc *rclone.Client, provider Provider) error {
	accountID, err := ui.ReadLine("Cloudflare account ID [optional]: ")
	if err != nil {
		return err
	}
	options := map[string]string{
		"provider": "Cloudflare",
		"env_auth": "false",
		"region":   "auto",
	}
	prefill := map[string]string{}
	if accountID != "" {
		prefill["endpoint"] = "https://" + accountID + ".r2.cloudflarestorage.com"
	}
	return createEditAuthenticate(rc, provider, options, prefill)
}

func setupB2(rc *rclone.Client, provider Provider) error {
	return createEditAuthenticate(rc, provider, nil, nil)
}

func setupOAuth(rc *rclone.Client, provider Provider, label string) error {
	fmt.Println()
	fmt.Println(label + " will authenticate in your browser after the config is saved.")
	return createEditAuthenticate(rc, provider, nil, nil)
}

func createEditAuthenticate(rc *rclone.Client, provider Provider, options, prefill map[string]string) error {
	configPath, err := rc.ConfigFile()
	if err != nil {
		return err
	}
	if err := validateExistingConfig(configPath); err != nil {
		return err
	}
	if err := rc.CreateRemote(provider.RcloneType, options); err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = rc.DeleteRemote()
		}
	}()

	if err := validateEditableConfig(configPath); err != nil {
		return err
	}
	for key, value := range prefill {
		if err := PatchValue(configPath, remoteName, key, value); err != nil {
			return fmt.Errorf("prepare rclone configuration: %w", err)
		}
	}

	fmt.Println()
	fmt.Println("Rclone configuration file:")
	fmt.Println(" ", configPath)
	fmt.Println()
	fmt.Println("The new [edrive-cloud] section is prepared.")
	fmt.Println("Edit that section as needed, save the file, and exit the editor.")
	fmt.Println("edrive will finish the provider setup automatically.")
	fmt.Println()

	if err := ui.EditFile(configPath); err != nil {
		return fmt.Errorf("edit rclone configuration: %w", err)
	}

	actualType, actualBackend, err := rc.RemoteIdentity()
	if err != nil {
		return err
	}
	if actualType != provider.RcloneType {
		return fmt.Errorf("edrive-cloud was changed to rclone type %q; expected %q", actualType, provider.RcloneType)
	}
	if provider.Backend != "" && !strings.EqualFold(actualBackend, provider.Backend) {
		return fmt.Errorf("edrive-cloud provider is %q; expected %q", actualBackend, provider.Backend)
	}

	if provider.OAuth {
		if err := rc.Reconnect(); err != nil {
			return err
		}
	}
	if err := rc.CheckConfigured(); err != nil {
		return err
	}

	cleanup = false
	fmt.Println()
	fmt.Println("Cloud provider configured:", provider.Name)
	fmt.Println("Rclone config saved:", configPath)
	return nil
}

func validateExistingConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read rclone configuration: %w", err)
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || strings.Contains(trimmed, "[") {
		return nil
	}
	return fmt.Errorf("rclone configuration is not a plain-text INI file; use the built-in rclone setup method instead")
}

func validateEditableConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read rclone configuration: %w", err)
	}
	if !strings.Contains(string(data), "["+remoteName+"]") {
		return fmt.Errorf("rclone configuration is not editable by this method (it may be encrypted); use the built-in rclone setup method instead")
	}
	return nil
}

func PatchValue(path, section, key, value string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	sectionStart := -1
	sectionEnd := len(lines)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "["+section+"]" {
			sectionStart = i
			continue
		}
		if sectionStart >= 0 && i > sectionStart && strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			sectionEnd = i
			break
		}
	}
	if sectionStart < 0 {
		return fmt.Errorf("rclone section %q not found", section)
	}

	valueLine := key + " = " + value
	for i := sectionStart + 1; i < sectionEnd; i++ {
		if existing, ok := splitINIKey(lines[i]); ok && strings.EqualFold(existing, key) {
			lines[i] = valueLine
			return writeConfig(path, lines)
		}
	}

	insertAt := sectionEnd
	for insertAt > sectionStart+1 && strings.TrimSpace(lines[insertAt-1]) == "" {
		insertAt--
	}
	lines = append(lines, "")
	copy(lines[insertAt+1:], lines[insertAt:])
	lines[insertAt] = valueLine
	return writeConfig(path, lines)
}

func writeConfig(path string, lines []string) error {
	target := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		target = resolved
	}
	info, err := os.Stat(target)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), ".rclone-config-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()

	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if _, err := tmp.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, target)
}

func splitINIKey(line string) (string, bool) {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return "", false
	}
	return strings.TrimSpace(parts[0]), true
}

func Providers() []Provider {
	out := append([]Provider(nil), providers...)
	sort.Slice(out, func(i, j int) bool {
		return out[i].Code < providers[j].Code
	})
	return out
}
