package modeltier

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Key sources reported by ResolveKey and ProviderStatus.KeySource.
const (
	KeySourceEnv  = "env"
	KeySourceNone = "none"
	// KeySourceStored (config.go) doubles as the source name.
)

// ErrCredentialMissing reports that credentials.json (or the provider's
// entry in it) does not exist.
var ErrCredentialMissing = errors.New("credential not found")

// DefaultCredentialsPath returns the global credentials.json path for homeDir.
func DefaultCredentialsPath(homeDir string) string {
	return filepath.Join(homeDir, ".glaude", "credentials.json")
}

// CredentialsPath returns the credentials.json next to this Store's config.toml.
func (s *Store) CredentialsPath() string {
	return filepath.Join(filepath.Dir(s.path), "credentials.json")
}

// readCredentialsFile decodes path into a generic map so unknown keys
// survive a rewrite. A missing or empty file yields an empty map. Parse
// errors never echo the file content.
func readCredentialsFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("modeltier: read %s: %w", path, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{}, nil
	}
	bad := fmt.Errorf("modeltier: %s is not a valid credentials file; fix or remove it", path)
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, bad
	}
	if doc == nil { // "null"
		return map[string]any{}, nil
	}
	raw, ok := doc.(map[string]any)
	if !ok {
		return nil, bad
	}
	// Shape check: never overwrite data we do not understand.
	if v, present := raw["providers"]; present && v != nil {
		providers, ok := v.(map[string]any)
		if !ok {
			return nil, bad
		}
		for _, entry := range providers {
			if _, ok := entry.(map[string]any); !ok {
				return nil, bad
			}
		}
	}
	return raw, nil
}

// CheckCredentials reports a credentials.json that exists but cannot be
// used (nil for a missing or valid file). The error never echoes content.
func CheckCredentials(path string) error {
	_, err := readCredentialsFile(path)
	return err
}

// ReadCredential returns the stored API key of provider id. A missing file
// or entry wraps ErrCredentialMissing.
func ReadCredential(path, id string) (string, error) {
	raw, err := readCredentialsFile(path)
	if err != nil {
		return "", err
	}
	providers, _ := raw["providers"].(map[string]any)
	entry, _ := providers[id].(map[string]any)
	key, _ := entry["api_key"].(string)
	if key == "" {
		return "", fmt.Errorf("modeltier: %s has no key for provider %s: %w", path, id, ErrCredentialMissing)
	}
	return key, nil
}

// WriteCredential stores key for provider id (an empty key removes the
// entry) with a 0600 atomic rewrite. Unknown keys are kept; a file that does
// not parse is never overwritten.
func WriteCredential(path, id, key string) error {
	raw, err := readCredentialsFile(path)
	if err != nil {
		return err
	}
	providers, _ := raw["providers"].(map[string]any)
	if providers == nil {
		providers = map[string]any{}
	}
	if key == "" {
		if _, ok := providers[id]; !ok {
			return nil
		}
		delete(providers, id)
	} else {
		entry, _ := providers[id].(map[string]any)
		if entry == nil {
			entry = map[string]any{}
		}
		entry["api_key"] = key
		providers[id] = entry
	}
	raw["providers"] = providers
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("modeltier: encode credentials: %w", err)
	}
	return writeAtomic(path, append(data, '\n'), 0o600, ".credentials.json.*")
}

// ResolveKey returns provider id's API key and where it came from:
// api_key_env (the variable must be set), then api_key_source = "stored"
// (credentials.json must hold the key), else no key. A stored provider whose
// key is missing is an error, never a silent "no key".
func ResolveKey(id string, p Provider, credPath string, getenv func(string) string) (key, source string, err error) {
	switch {
	case p.APIKeyEnv != "":
		if key = getenv(p.APIKeyEnv); key == "" {
			return "", KeySourceEnv, fmt.Errorf("environment variable %s is not set", p.APIKeyEnv)
		}
		return key, KeySourceEnv, nil
	case p.APIKeySource == KeySourceStored:
		key, err = ReadCredential(credPath, id)
		if err != nil {
			if errors.Is(err, ErrCredentialMissing) {
				return "", KeySourceStored, fmt.Errorf("credentials.json has no key for provider %s; re-enter it with /provider: %w", id, ErrCredentialMissing)
			}
			return "", KeySourceStored, fmt.Errorf("%w; re-enter the key with /provider", err)
		}
		return key, KeySourceStored, nil
	}
	return "", KeySourceNone, nil
}
