package modeltier

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func credPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), ".glaude", "credentials.json")
}

func TestDefaultCredentialsPath(t *testing.T) {
	if got, want := DefaultCredentialsPath("/h"), filepath.Join("/h", ".glaude", "credentials.json"); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestCredentialRoundTripAndMode(t *testing.T) {
	p := credPath(t)
	if err := WriteCredential(p, "zai", "sk-123"); err != nil {
		t.Fatal(err)
	}
	got, err := ReadCredential(p, "zai")
	if err != nil || got != "sk-123" {
		t.Fatalf("Read = %q, %v", got, err)
	}
	info, err := os.Stat(p)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}

func TestWriteCredentialPreservesUnknownKeys(t *testing.T) {
	p := credPath(t)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte(`{"other":1,"providers":{"b":{"api_key":"kb","note":"x"}}}`), 0o600)
	if err := WriteCredential(p, "a", "ka"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	for _, want := range []string{`"other"`, `"note"`, `"kb"`, `"ka"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("credentials.json lost %s: %s", want, data)
		}
	}
}

func TestWriteCredentialEmptyDeletes(t *testing.T) {
	p := credPath(t)
	_ = WriteCredential(p, "a", "ka")
	if err := WriteCredential(p, "a", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCredential(p, "a"); !errors.Is(err, ErrCredentialMissing) {
		t.Fatalf("after delete err = %v, want ErrCredentialMissing", err)
	}
	// Deleting from a missing file is a no-op.
	if err := WriteCredential(credPath(t), "x", ""); err != nil {
		t.Fatalf("delete on missing file: %v", err)
	}
}

func TestCredentialCorruptFile(t *testing.T) {
	p := credPath(t)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte(`{"secret-ish`), 0o600)
	_, err := ReadCredential(p, "a")
	if err == nil || errors.Is(err, ErrCredentialMissing) || strings.Contains(err.Error(), "secret-ish") {
		t.Fatalf("Read err = %v; want parse error without content", err)
	}
	if err := WriteCredential(p, "a", "k"); err == nil {
		t.Fatal("Write on corrupt file should fail")
	}
	data, _ := os.ReadFile(p)
	if string(data) != `{"secret-ish` {
		t.Fatalf("corrupt file was rewritten: %s", data)
	}
}

func TestResolveKey(t *testing.T) {
	p := credPath(t)
	_ = WriteCredential(p, "s", "stored-key")
	_ = WriteCredential(p, "n", "orphan-key")
	env := map[string]string{"SET": "env-key"}
	getenv := func(k string) string { return env[k] }

	cases := []struct {
		name       string
		id         string
		prov       Provider
		key, src   string
		wantErrSub string
	}{
		{"env set", "e", Provider{APIKeyEnv: "SET"}, "env-key", KeySourceEnv, ""},
		{"env unset", "e", Provider{APIKeyEnv: "UNSET"}, "", KeySourceEnv, "UNSET"},
		{"stored", "s", Provider{APIKeySource: KeySourceStored}, "stored-key", KeySourceStored, ""},
		{"stored missing", "m", Provider{APIKeySource: KeySourceStored}, "", KeySourceStored, "/provider"},
		// credentials.json has an entry but config.toml has no marker: no key.
		{"none ignores orphan", "n", Provider{}, "", KeySourceNone, ""},
	}
	for _, c := range cases {
		key, src, err := ResolveKey(c.id, c.prov, p, getenv)
		if key != c.key || src != c.src {
			t.Errorf("%s: got (%q,%q)", c.name, key, src)
		}
		if c.wantErrSub == "" && err != nil || c.wantErrSub != "" && (err == nil || !strings.Contains(err.Error(), c.wantErrSub)) {
			t.Errorf("%s: err = %v, want containing %q", c.name, err, c.wantErrSub)
		}
	}
	// Corrupt credentials.json → stored provider errors (never falls back to no key).
	cp := credPath(t)
	_ = os.MkdirAll(filepath.Dir(cp), 0o755)
	_ = os.WriteFile(cp, []byte("{"), 0o600)
	if _, _, err := ResolveKey("s", Provider{APIKeySource: KeySourceStored}, cp, getenv); err == nil {
		t.Fatal("corrupt credentials: want error")
	}
}

func TestStore_ProvidersKeySource(t *testing.T) {
	t.Setenv("ZAI_API_KEY", "k")
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	_ = os.WriteFile(cfgPath, []byte(`
[providers.zai]
format = "openai"
base_url = "x"
api_key_env = "ZAI_API_KEY"
[providers.st]
format = "openai"
base_url = "x"
api_key_source = "stored"
[providers.gone]
format = "openai"
base_url = "x"
api_key_source = "stored"
[providers.local]
format = "openai"
base_url = "x"
`), 0o644)
	s, w := Load(cfgPath)
	if len(w) != 0 {
		t.Fatal(w)
	}
	if s.CredentialsPath() != filepath.Join(dir, "credentials.json") {
		t.Fatalf("CredentialsPath = %q", s.CredentialsPath())
	}
	_ = WriteCredential(s.CredentialsPath(), "st", "k2")
	got := map[string]ProviderStatus{}
	for _, p := range s.Providers() {
		got[p.ID] = p
	}
	check := func(id, src string, present bool) {
		if got[id].KeySource != src || got[id].KeyPresent != present {
			t.Errorf("%s = %+v, want source %s present %v", id, got[id], src, present)
		}
	}
	check("zai", KeySourceEnv, true)
	check("st", KeySourceStored, true)
	check("gone", KeySourceStored, false)
	check("local", KeySourceNone, true)
}
