package voicetype

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

type memoryCredentials struct {
	values  map[string]string
	failGet bool
	failSet bool
	failDel bool
	skipSet bool
	skipDel bool
	onSet   func()
}

func (m *memoryCredentials) Set(provider, key string) error {
	if m.onSet != nil {
		m.onSet()
	}
	if m.failSet {
		return errors.New("keyring unavailable")
	}
	if m.skipSet {
		return nil
	}
	m.values[provider] = key
	return nil
}

func (m *memoryCredentials) Get(provider string) (string, error) {
	if m.failGet {
		return "", errors.New("keyring locked")
	}
	key, ok := m.values[provider]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return key, nil
}

func (m *memoryCredentials) Delete(provider string) error {
	if m.failDel {
		return errors.New("keyring unavailable")
	}
	if m.skipDel {
		return nil
	}
	if _, ok := m.values[provider]; !ok {
		return keyring.ErrNotFound
	}
	delete(m.values, provider)
	return nil
}

func useCredentialStore(t *testing.T, store credentialStore) {
	t.Helper()
	previous := providerKeys
	providerKeys = store
	t.Cleanup(func() { providerKeys = previous })
}

func TestProviderAPIKeysAreSharedByProfilesAndSnapshotPerRecording(t *testing.T) {
	settingsTestHome(t)
	store := &memoryCredentials{values: map[string]string{}}
	useCredentialStore(t, store)
	if err := SaveAPIKey(providerOpenAI, "personal-key"); err != nil {
		t.Fatal(err)
	}
	view := readSettingsTest(t)
	view, err := UpsertProfile(ProfileUpdate{
		Revision: view.Revision,
		Name:     "Personal",
		Provider: providerOpenAI,
	})
	if err != nil {
		t.Fatal(err)
	}
	first := view.ActiveProfile
	view, err = UpsertProfile(ProfileUpdate{
		Revision: view.Revision,
		Name:     "Second OpenAI",
		Provider: providerOpenAI,
	})
	if err != nil {
		t.Fatal(err)
	}
	second := view.Profiles[1].ID
	cfg, err := configReadStrict()
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := cfg.resolveSTT("")
	if err != nil || snapshot.APIKey != "personal-key" {
		t.Fatal("active profile did not use the shared provider key", err)
	}
	if err := SaveAPIKey(providerOpenAI, "replacement-key"); err != nil {
		t.Fatal(err)
	}
	if snapshot.APIKey != "personal-key" {
		t.Fatal("existing recording snapshot changed")
	}
	if _, err := SelectProfile(view.Revision, second); err != nil {
		t.Fatal(err)
	}
	cfg, err = configReadStrict()
	if err != nil {
		t.Fatal(err)
	}
	next, err := cfg.resolveSTT("")
	if err != nil || next.APIKey != "replacement-key" {
		t.Fatal("other profile did not reuse the replacement key", err)
	}
	if _, err := SelectProfile(view.Revision, first); err == nil {
		t.Fatal("stale revision accepted")
	}
}

func TestProviderSwitchClearsIncompatibleSettingsAndRestoresCloudDefaults(t *testing.T) {
	settingsTestHome(t)
	useCredentialStore(t, &memoryCredentials{values: map[string]string{providerOpenAI: "key"}})
	view := readSettingsTest(t)
	view, err := UpsertProfile(ProfileUpdate{
		Revision:      view.Revision,
		Name:          "Personal",
		Provider:      providerOpenAI,
		StreamingMode: streamingModeNone,
		Languages:     ptr([]string{"en", "de"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	id := view.ActiveProfile
	view, err = UpsertProfile(ProfileUpdate{
		Revision:      view.Revision,
		ID:            id,
		Name:          "Personal",
		Provider:      providerLocal,
		ModelID:       localModelUnifiedEnglish,
		StreamingMode: streamingModeLive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Profiles[0].Provider != providerLocal ||
		view.Profiles[0].ModelID != localModelUnifiedEnglish ||
		view.Profiles[0].StreamingMode != streamingModeLive {
		t.Fatalf("local switch lost defaults or identity: %+v", view.Profiles[0])
	}
	view, err = UpsertProfile(ProfileUpdate{
		Revision: view.Revision,
		ID:       id,
		Name:     "Personal",
		Provider: providerOpenAI,
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Profiles[0].StreamingMode != streamingModeProcess ||
		len(view.Profiles[0].Languages) != 0 {
		t.Fatalf("cloud switch retained local fields: %+v", view.Profiles[0])
	}
	raw, err := os.ReadFile(configFilePath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"model_id"`) || strings.Contains(string(raw), `"api_key"`) {
		t.Fatal("provider switch left stale profile fields or a plaintext key")
	}
}

func TestEngineAppliesSameProfileProviderSwitchToNextRecording(t *testing.T) {
	setupEngineTest(t)
	if err := SaveAPIKey(providerOpenAI, "cloud-key"); err != nil {
		t.Fatal(err)
	}
	view := readSettingsTest(t)
	view, err := UpsertProfile(ProfileUpdate{
		Revision: view.Revision,
		Name:     "Personal",
		Provider: providerOpenAI,
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := startEngine("test")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if service.stt.settings.Provider != providerOpenAI ||
		service.stt.settings.APIKey != "cloud-key" {
		t.Fatalf("initial engine profile: %+v", service.stt.settings)
	}
	view, err = UpsertProfile(ProfileUpdate{
		Revision:      view.Revision,
		ID:            view.ActiveProfile,
		Name:          "Personal",
		Provider:      providerLocal,
		ModelID:       localModelUnifiedEnglish,
		StreamingMode: streamingModeNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	service.RefreshSettings()
	if service.stt.settings.Provider != providerLocal ||
		service.stt.settings.Model != localModelUnifiedEnglish ||
		service.stt.settings.APIKey != "" {
		t.Fatalf("engine did not apply provider switch: %+v", service.stt.settings)
	}
	view, err = UpsertProfile(ProfileUpdate{
		Revision: view.Revision,
		ID:       view.ActiveProfile,
		Name:     "Personal",
		Provider: providerOpenAI,
	})
	if err != nil {
		t.Fatal(err)
	}
	service.RefreshSettings()
	if service.stt.settings.Provider != providerOpenAI ||
		service.stt.settings.APIKey != "cloud-key" {
		t.Fatalf("engine did not restore cloud key after switch: %+v", service.stt.settings)
	}
}

func TestAPIKeyFailuresAreActionableAndDoNotExposeSecrets(t *testing.T) {
	settingsTestHome(t)
	store := &memoryCredentials{values: map[string]string{}, failSet: true}
	useCredentialStore(t, store)
	err := SaveAPIKey(providerOpenAI, "private-secret")
	if err == nil || !strings.Contains(err.Error(), "system keyring") || strings.Contains(err.Error(), "private-secret") {
		t.Fatalf("unexpected save error: %v", err)
	}
	store.failSet = false
	if err := SaveAPIKey(providerOpenAI, "private-secret"); err != nil {
		t.Fatal(err)
	}
	store.failGet = true
	view := readSettingsTest(t)
	if view.APIKeys[providerOpenAI].Error == "" ||
		strings.Contains(view.APIKeys[providerOpenAI].Error, "private-secret") {
		t.Fatalf("keyring failure was hidden or exposed: %+v", view.APIKeys[providerOpenAI])
	}
	if _, _, err := providerAPIKey(providerOpenAI); err == nil {
		t.Fatal("keyring read failure hidden")
	}
}

func TestReplacingAProviderKeyLeavesOneCurrentSecret(t *testing.T) {
	settingsTestHome(t)
	store := &memoryCredentials{values: map[string]string{}}
	useCredentialStore(t, store)
	if err := SaveAPIKey(providerOpenAI, "old-key"); err != nil {
		t.Fatal(err)
	}
	store.failSet = true
	if err := SaveAPIKey(providerOpenAI, "new-key"); err == nil {
		t.Fatal("replacement failure was hidden")
	}
	if key, saved, err := providerAPIKey(providerOpenAI); err != nil || !saved || key != "old-key" {
		t.Fatalf("failed replacement did not restore the previous key: %q %v %v", key, saved, err)
	}
	store.failSet = false
	if err := SaveAPIKey(providerOpenAI, "new-key"); err != nil {
		t.Fatal(err)
	}
	if key, saved, err := providerAPIKey(providerOpenAI); err != nil || !saved || key != "new-key" {
		t.Fatalf("replacement did not publish the new key: %q %v %v", key, saved, err)
	}
}

func TestProfileSaveFailureDoesNotPublishAProviderSwitch(t *testing.T) {
	settingsTestHome(t)
	useCredentialStore(t, &memoryCredentials{values: map[string]string{providerOpenAI: "key"}})
	view := readSettingsTest(t)
	view, err := UpsertProfile(ProfileUpdate{
		Revision: view.Revision,
		Name:     "Personal",
		Provider: providerOpenAI,
	})
	if err != nil {
		t.Fatal(err)
	}
	id := view.ActiveProfile
	data, err := os.ReadFile(configFilePath())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(os.Getenv("HOME"), 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(os.Getenv("HOME"), 0700) })
	if _, err := UpsertProfile(ProfileUpdate{
		Revision:      view.Revision,
		ID:            id,
		Name:          "Personal",
		Provider:      providerLocal,
		ModelID:       localModelUnifiedEnglish,
		StreamingMode: streamingModeProcess,
	}); err == nil {
		t.Fatal("read-only profile write unexpectedly succeeded")
	}
	after, err := os.ReadFile(configFilePath())
	if err != nil || string(after) != string(data) {
		t.Fatal("failed save changed the on-disk profile")
	}
}

func TestInlineKeyMigrationIsAtomicAndRejectsCollisions(t *testing.T) {
	settingsTestHome(t)
	store := &memoryCredentials{values: map[string]string{}}
	useCredentialStore(t, store)
	data := []byte(`{"profiles":[{"id":"one","name":"One","provider":"openai","config":{"api_key":"first","future":true}},{"id":"two","name":"Two","provider":"openai","config":{"api_key":"second"}}],"active_profile":"one"}`)
	if err := os.WriteFile(configFilePath(), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSettings(); err == nil || !strings.Contains(err.Error(), "different API keys") {
		t.Fatalf("collision was not reported: %v", err)
	}
	if len(store.values) != 0 {
		t.Fatal("migration wrote before validating the complete config")
	}
	after, err := os.ReadFile(configFilePath())
	if err != nil || string(after) != string(data) {
		t.Fatal("failed migration changed the original config")
	}
}

func TestUnverifiedKeyringWriteKeepsInlineCredential(t *testing.T) {
	settingsTestHome(t)
	store := &memoryCredentials{values: map[string]string{}, skipSet: true}
	useCredentialStore(t, store)
	data := []byte(`{"profiles":[{"id":"one","name":"One","provider":"openai","config":{"api_key":"old-key"}}],"active_profile":"one"}`)
	if err := os.WriteFile(configFilePath(), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSettings(); err == nil || !strings.Contains(err.Error(), "verify") {
		t.Fatalf("unverified migration was not rejected: %v", err)
	}
	after, err := os.ReadFile(configFilePath())
	if err != nil || string(after) != string(data) {
		t.Fatal("unverified migration changed the original config")
	}
}

func TestCancelledKeyringDeletionReportsFailure(t *testing.T) {
	settingsTestHome(t)
	store := &memoryCredentials{values: map[string]string{providerOpenAI: "saved-key"}, skipDel: true}
	useCredentialStore(t, store)
	if err := DeleteAPIKey(providerOpenAI); err == nil || !strings.Contains(err.Error(), "still in the system keyring") {
		t.Fatalf("cancelled deletion was not reported: %v", err)
	}
}

func TestSavingKeyMigratesInlineKeyBeforeReplacement(t *testing.T) {
	settingsTestHome(t)
	store := &memoryCredentials{values: map[string]string{}}
	useCredentialStore(t, store)
	data := []byte(`{"profiles":[{"id":"one","name":"One","provider":"openai","config":{"api_key":"old-key"}}],"active_profile":"one"}`)
	if err := os.WriteFile(configFilePath(), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := SaveAPIKey(providerOpenAI, "new-key"); err != nil {
		t.Fatal(err)
	}
	if key, saved, err := providerAPIKey(providerOpenAI); err != nil || !saved || key != "new-key" {
		t.Fatal("replacement lost during migration", err)
	}
	if _, err := ReadSettings(); err != nil {
		t.Fatalf("key replacement left a migration conflict: %v", err)
	}
}

func TestMixedLegacyAndProfileKeysArePreserved(t *testing.T) {
	tests := []struct {
		name       string
		document   string
		wantRouter bool
		wantError  bool
	}{
		{"distinct providers", `{"profiles":[{"id":"one","name":"One","provider":"openai","config":{"api_key":"openai-key"}}],"active_profile":"one","api_keys":{"openrouter":"router-key"}}`, true, false},
		{"conflicting keys", `{"profiles":[{"id":"one","name":"One","provider":"openai","config":{"api_key":"openai-key"}}],"active_profile":"one","api_keys":{"openai":"different-key"}}`, false, true},
		{"ambiguous key", `{"profiles":[{"id":"one","name":"One","provider":"openai","config":{}}],"active_profile":"one","api_key":"unknown-key"}`, false, true},
		{"unrecognized provider", `{"profiles":[],"active_profile":"","api_keys":{"other":"unknown-key"}}`, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settingsTestHome(t)
			useCredentialStore(t, &memoryCredentials{values: map[string]string{}})
			if err := os.WriteFile(configFilePath(), []byte(tt.document), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := ReadSettings()
			if tt.wantError {
				if err == nil {
					t.Fatal("ambiguous credentials were silently discarded")
				}
				data, readErr := os.ReadFile(configFilePath())
				if readErr != nil || string(data) != tt.document {
					t.Fatal("failed migration changed the original settings")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, saved, err := providerAPIKey(providerOpenRouter); err != nil || saved != tt.wantRouter {
				t.Fatal("legacy provider key was not migrated", err)
			}
		})
	}
}

func TestRetryRemovesCredentialFromStaleRecoveryBackup(t *testing.T) {
	settingsTestHome(t)
	useCredentialStore(t, &memoryCredentials{values: map[string]string{providerOpenAI: "saved-key"}})
	clean := []byte(`{"profiles":[{"id":"one","name":"One","provider":"openai","config":{}}],"active_profile":"one"}`)
	stale := []byte(`{"profiles":[{"id":"one","name":"One","provider":"openai","config":{"api_key":"saved-key"}}],"active_profile":"one"}`)
	if err := os.WriteFile(configFilePath(), clean, 0600); err != nil {
		t.Fatal(err)
	}
	backupDir, err := BackupDirectory()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "config.jsonc"), stale, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSettings(); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(filepath.Join(backupDir, "config.jsonc"))
	if err != nil || strings.Contains(string(backup), "saved-key") {
		t.Fatal("stale recovery backup retained the inline key")
	}
}

func TestLegacyMigrationLeavesOriginalWhenKeyringConflicts(t *testing.T) {
	settingsTestHome(t)
	store := &memoryCredentials{values: map[string]string{providerOpenRouter: "different-key"}}
	useCredentialStore(t, store)
	data := []byte(`{"provider":"openai","api_keys":{"openai":"old-openai","openrouter":"old-router"},"future":true}`)
	if err := os.WriteFile(configFilePath(), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSettings(); err == nil || !strings.Contains(err.Error(), "system password manager") {
		t.Fatalf("keyring conflict was not actionable: %v", err)
	}
	after, err := os.ReadFile(configFilePath())
	if err != nil || string(after) != string(data) {
		t.Fatal("failed legacy migration changed the original config")
	}
	if _, exists := store.values[providerOpenAI]; exists {
		t.Fatal("migration wrote a key before checking all providers")
	}
}

func TestInlineMigrationPreservesConcurrentConfigEdits(t *testing.T) {
	settingsTestHome(t)
	data := []byte(`{"profiles":[{"id":"one","name":"One","provider":"openai","config":{"api_key":"old-key"}}],"active_profile":"one"}`)
	newer := []byte(`{"profiles":[],"active_profile":"","future":"concurrent edit"}`)
	if err := os.WriteFile(configFilePath(), data, 0600); err != nil {
		t.Fatal(err)
	}
	store := &memoryCredentials{values: map[string]string{}}
	store.onSet = func() {
		if err := os.WriteFile(configFilePath(), newer, 0600); err != nil {
			t.Fatal(err)
		}
	}
	useCredentialStore(t, store)
	if _, err := ReadSettings(); err == nil || !strings.Contains(err.Error(), "changed elsewhere") {
		t.Fatalf("concurrent edit was not reported: %v", err)
	}
	after, err := os.ReadFile(configFilePath())
	if err != nil || string(after) != string(newer) {
		t.Fatal("migration overwrote the concurrent config edit")
	}
}

func TestEmptyInlineKeysAreRemoved(t *testing.T) {
	settingsTestHome(t)
	useCredentialStore(t, &memoryCredentials{values: map[string]string{}})
	data := []byte(`{"profiles":[{"id":"one","name":"One","provider":"openai","config":{"api_key":""}}],"active_profile":"one"}`)
	if err := os.WriteFile(configFilePath(), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSettings(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(configFilePath())
	if err != nil || strings.Contains(string(after), `"api_key"`) {
		t.Fatal("empty legacy key field remained in config")
	}
}

func TestDamagedInlineKeyRecoversWithoutLosingProfile(t *testing.T) {
	settingsTestHome(t)
	useCredentialStore(t, &memoryCredentials{values: map[string]string{}})
	valid := []byte(`{"profiles":[{"id":"one","name":"One","provider":"openai","config":{"api_key":"recoverable-key","future":true}}],"active_profile":"one"}`)
	broken := []byte(`{"profiles":[{"id":"one","name":"One","provider":"openai","config":{"api_key":42}}],"active_profile":"one"}`)
	backupDir, err := BackupDirectory()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "config.jsonc"), valid, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configFilePath(), broken, 0600); err != nil {
		t.Fatal(err)
	}
	view, err := ReadSettings()
	if err != nil || len(view.Profiles) != 1 || view.ActiveProfile != "one" || !view.APIKeys[providerOpenAI].Saved || view.Notice == "" {
		t.Fatalf("valid profile backup was not recovered: %+v, %v", view, err)
	}
	for _, path := range []string{configFilePath(), filepath.Join(backupDir, "config.jsonc")} {
		data, err := os.ReadFile(path)
		if err != nil || strings.Contains(string(data), "recoverable-key") {
			t.Fatal("recovered config retained the inline credential")
		}
	}
}

func TestSystemKeyringRoundTrip(t *testing.T) {
	if os.Getenv("VOICE_TYPE_TEST_SYSTEM_KEYRING") != "1" {
		t.Skip("set VOICE_TYPE_TEST_SYSTEM_KEYRING=1 with an isolated Secret Service")
	}
	previousService, previousStore := credentialService, providerKeys
	credentialService = "com.eriknovikov.voice-type.test-" + t.Name()
	providerKeys = systemCredentialStore{}
	t.Cleanup(func() {
		_ = providerKeys.(systemCredentialStore).Delete(providerOpenAI)
		credentialService, providerKeys = previousService, previousStore
	})
	if err := SaveAPIKey(providerOpenAI, "isolated-test-key"); err != nil {
		t.Fatal(err)
	}
	key, saved, err := providerAPIKey(providerOpenAI)
	if err != nil || !saved || key != "isolated-test-key" {
		t.Fatalf("system keyring round trip: %q %v %v", key, saved, err)
	}
	if err := SaveAPIKey(providerOpenAI, "isolated-replacement"); err != nil {
		t.Fatal(err)
	}
	key, saved, err = providerAPIKey(providerOpenAI)
	if err != nil || !saved || key != "isolated-replacement" {
		t.Fatalf("system keyring replacement: %q %v %v", key, saved, err)
	}
	if err := DeleteAPIKey(providerOpenAI); err != nil {
		t.Fatal(err)
	}
	if _, saved, err := providerAPIKey(providerOpenAI); err != nil || saved {
		t.Fatalf("system keyring deletion: %v %v", saved, err)
	}
}
