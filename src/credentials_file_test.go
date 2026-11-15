package voicetype

import (
	"bytes"
	"encoding/base64"
	"os"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestMinimalCredentialFileMode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	previous := providerKeys
	providerKeys = preferredCredentialStore{}
	t.Cleanup(func() { providerKeys = previous })

	if err := setCredentialStorage(storageFile); err != nil {
		t.Fatal(err)
	}
	if got := currentCredentialStorage(); got != storageFile {
		t.Fatalf("storage = %s", got)
	}
	if err := SaveAPIKey(providerOpenAI, "sk-minimal"); err != nil {
		t.Fatal(err)
	}
	path, err := credentialPath(providerOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("sk-minimal")) {
		t.Fatal("file store wrote the plaintext key")
	}
	decoded, err := base64.StdEncoding.DecodeString(string(bytes.TrimSpace(data)))
	if err != nil || string(decoded) != "sk-minimal" {
		t.Fatalf("base64 roundtrip: %q %v", decoded, err)
	}
	if key, saved, err := providerAPIKey(providerOpenAI); err != nil || !saved || key != "sk-minimal" {
		t.Fatalf("read back: %q %v %v", key, saved, err)
	}
	if err := DeleteAPIKey(providerOpenAI); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("key file survived deletion: %v", err)
	}
}

func TestMinimalDoctorAndCommand(t *testing.T) {
	if command, err := ParseCommand([]string{"doctor"}); err != nil || command != CommandDoctor {
		t.Fatalf("doctor command: %v %v", command, err)
	}
	var out bytes.Buffer
	if code := RunDoctor("test", &out); code != 0 {
		t.Fatalf("doctor exit %d", code)
	}
	if !bytes.Contains(out.Bytes(), []byte("Voice Type diagnostics vtest")) {
		t.Fatalf("doctor output: %s", out.String())
	}
}

func TestStorageSwitchCopiesKeys(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	keyring.MockInit()
	previous := providerKeys
	providerKeys = preferredCredentialStore{}
	t.Cleanup(func() { providerKeys = previous })

	if err := keyring.Set(credentialService, providerOpenAI, "sk-from-keyring"); err != nil {
		t.Fatal(err)
	}
	if err := setCredentialStorage(storageFile); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(mustCredentialPath(t, providerOpenAI))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(string(bytes.TrimSpace(data)))
	if err != nil || string(decoded) != "sk-from-keyring" {
		t.Fatalf("keyring key was not copied to the file: %q %v", decoded, err)
	}

	if err := keyring.Delete(credentialService, providerOpenAI); err != nil {
		t.Fatal(err)
	}
	if err := setCredentialStorage(storageKeyring); err != nil {
		t.Fatal(err)
	}
	if key, err := keyring.Get(credentialService, providerOpenAI); err != nil || key != "sk-from-keyring" {
		t.Fatalf("file key was not copied to the keyring: %q %v", key, err)
	}
}

func mustCredentialPath(t *testing.T, provider string) string {
	t.Helper()
	path, err := credentialPath(provider)
	if err != nil {
		t.Fatal(err)
	}
	return path
}
