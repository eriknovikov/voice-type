package voicetype

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func setupEngineTest(t *testing.T) int {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	if err := os.WriteFile(configFilePath(), []byte(fmt.Sprintf(`{"provider":"openai","port":%d}`, port)), 0600); err != nil {
		t.Fatal(err)
	}
	return port
}

func TestEngineLifecycleAndIsolation(t *testing.T) {
	port := setupEngineTest(t)
	service, err := startEngine("test")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if status := service.Status(); status.State != "idle" || status.Error == "" {
		t.Fatalf("setup status: %+v", status)
	}
	if _, err := os.Stat(filepath.Join(engineRuntimeDir(), "desktop.sock")); !os.IsNotExist(err) {
		t.Fatal("embedded service created a management socket")
	}
	if _, err := startEngine("second"); err == nil {
		t.Fatal("second service adopted or replaced the first")
	}
	// There is no embedded TCP interface, even on the configured CLI port.
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal("embedded service reserved a TCP port", err)
	}
	defer listener.Close()
	if err := service.Toggle(); err == nil {
		t.Fatal("missing credentials must block direct capture")
	}
	settings, err := ReadSettings()
	if err != nil {
		t.Fatal(err)
	}
	key := "test-key"
	_, err = UpsertProfile(ProfileUpdate{Revision: settings.Revision, Name: "OpenAI 1", Provider: "openai", APIKey: &key})
	service.RefreshSettings()
	if err != nil {
		t.Fatal(err)
	}
	if service.Status().Error != "" {
		t.Fatal("saved credentials did not refresh service")
	}
	logs, err := service.Logs()
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) == 0 {
		t.Fatal("missing diagnostics")
	}
	if err := service.ClearLogs(); err != nil {
		t.Fatal(err)
	}
	service.Close()
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); service.Close() }()
	}
	wg.Wait()
	// The stable lock inode remains; ownership does not.
	lock, err := acquireEngineLock()
	if err != nil {
		t.Fatal("engine lock leaked", err)
	}
	lock.Close()
}

func TestEngineShutdownDiscardsRecording(t *testing.T) {
	setupEngineTest(t)
	settings, _ := ReadSettings()
	key := "test-key"
	if _, err := UpsertProfile(ProfileUpdate{Revision: settings.Revision, Name: "OpenAI 1", Provider: "openai", APIKey: &key}); err != nil {
		t.Fatal(err)
	}
	service, err := startEngine("test")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	service.recorder = &fixtureRecorder{}
	if err := service.Toggle(); err != nil {
		t.Fatal(err)
	}
	if service.Status().State != "recording" {
		t.Fatal("not recording")
	}
	service.Close()
	if service.Status().State != "idle" {
		t.Fatal("recording not released")
	}
	if err := service.Toggle(); err == nil {
		t.Fatal("capture allowed after shutdown")
	}
}

func TestEngineIgnoresOccupiedLegacyPort(t *testing.T) {
	port := setupEngineTest(t)
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	service, err := startEngine("test")
	if err != nil {
		t.Fatal(err)
	}
	service.Close()
}

func TestEngineStartupConflictPreservesFiles(t *testing.T) {
	setupEngineTest(t)
	lock, err := acquireEngineLock()
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	before, _ := os.ReadFile(configFilePath())
	if _, err := startEngine("test"); err == nil || !strings.Contains(err.Error(), "another Voice Type engine") {
		t.Fatalf("conflict error: %v", err)
	}
	after, _ := os.ReadFile(configFilePath())
	if string(before) != string(after) {
		t.Fatal("conflict changed config")
	}
	if _, err := os.Stat(runLogFilePath()); !os.IsNotExist(err) {
		t.Fatal("conflict opened logs")
	}
}

// Opt-in local level capture only: no audio is saved or sent to a provider.
func TestEmbeddedMicrophoneLifecycle(t *testing.T) {
	if os.Getenv("VOICE_TYPE_TEST_REAL_AUDIO") != "1" {
		t.Skip("set VOICE_TYPE_TEST_REAL_AUDIO=1")
	}
	audioServer := os.Getenv("PULSE_SERVER")
	if audioServer == "" {
		audioServer = "unix:" + filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "pulse/native")
	}
	setupEngineTest(t)
	t.Setenv("PULSE_SERVER", audioServer)
	service, err := startEngine("test")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if err := service.StartMicrophoneTest(""); err != nil {
		t.Fatal(err)
	}
	for range 20 {
		status := service.Status()
		if status.State != "testing" || status.Level < 0 || status.Level > 1 {
			t.Fatalf("invalid meter: %+v", status)
		}
		time.Sleep(time.Second / 30)
	}
	if err := service.Toggle(); err == nil {
		t.Fatal("dictation interrupted microphone test")
	}
	// Closing the host also releases an active microphone test.
	service.Close()
	status := service.Status()
	if status.State != "idle" || status.Level != 0 {
		t.Fatalf("microphone leaked: %+v", status)
	}
}

func TestSettingsPreserveIgnoredLegacyPort(t *testing.T) {
	port := setupEngineTest(t)
	service, err := startEngine("test")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	settings, err := ReadSettings()
	if err != nil {
		t.Fatal(err)
	}
	microphone := "saved-source"
	if _, err := service.SaveSettings(SettingsUpdate{Revision: settings.Revision, Microphone: &microphone}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(configFilePath())
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := readConfigDocument()
	if err != nil || string(raw["port"]) != fmt.Sprint(port) {
		t.Fatalf("legacy metadata changed: %s %v", data, err)
	}
	if _, err := configParse([]byte(`{"profiles":[],"port":"obsolete-invalid-value"}`)); err != nil {
		t.Fatal("obsolete port invalidated config", err)
	}
}

type waitingRecorder struct{ entered, release chan struct{} }

func (r *waitingRecorder) Start() error           { close(r.entered); <-r.release; return nil }
func (r *waitingRecorder) Stop() ([]int16, error) { return make([]int16, 3200), nil }
func (r *waitingRecorder) Close()                 {}
func TestEmbeddedToggleAsyncStateRules(t *testing.T) {
	setupEngineTest(t)
	settings, _ := ReadSettings()
	key := "fixture-key"
	if _, err := UpsertProfile(ProfileUpdate{Revision: settings.Revision, Name: "Test", Provider: "openai", APIKey: &key}); err != nil {
		t.Fatal(err)
	}
	service, err := startEngine("test")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	recorder := &waitingRecorder{entered: make(chan struct{}), release: make(chan struct{})}
	service.recorder = recorder
	started := service.ToggleAsync()
	<-recorder.entered
	if err := <-service.ToggleAsync(); err != nil {
		t.Fatal("checking cancellation:", err)
	}
	close(recorder.release)
	if err := <-started; err == nil {
		t.Fatal("cancelled checking became a recording")
	}
	if service.Status().State != "idle" {
		t.Fatal("cancelled capture not idle")
	}
	service.recorder = &fixtureRecorder{}
	if err := <-service.ToggleAsync(); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"text":""}`)
	}))
	defer server.Close()
	service.stt.endpoint = server.URL
	stopped := service.ToggleAsync()
	<-entered
	for range 10 {
		if err := <-service.ToggleAsync(); err == nil || !strings.Contains(err.Error(), "transcribing") {
			t.Fatal("accepted during transcription", err)
		}
	}
	close(release)
	<-stopped
	if service.Status().State != "idle" {
		t.Fatal("replayed a rejected command")
	}
}

func TestDelayedStopNeverStartsANewSession(t *testing.T) {
	setupEngineTest(t)
	settings, _ := ReadSettings()
	key := "fixture-key"
	if _, err := UpsertProfile(ProfileUpdate{Revision: settings.Revision, Name: "Test", Provider: "openai", APIKey: &key}); err != nil {
		t.Fatal(err)
	}
	service, err := startEngine("test")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	service.recorder = &failureRecorder{} // Empty audio: no provider request or insertion.
	if err := service.Toggle(); err != nil {
		t.Fatal(err)
	}
	stop, err := service.prepareToggle()
	if err != nil || stop == nil {
		t.Fatal(err)
	}
	// A duration cap completes the same recording before this stop is scheduled.
	service.stopSessionMatching(service.sessionID, true)
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if service.Status().State != "idle" {
		t.Fatal("delayed stop unexpectedly started recording")
	}
}

func TestCloseCancelsTranscriptionAndDrainsEngine(t *testing.T) {
	setupEngineTest(t)
	settings, _ := ReadSettings()
	key := "fixture-key"
	if _, err := UpsertProfile(ProfileUpdate{Revision: settings.Revision, Provider: "openai", Name: "Test", APIKey: &key}); err != nil {
		t.Fatal(err)
	}
	engine, err := startEngine("test")
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	engine.recorder = &fixtureRecorder{}
	if err := engine.Toggle(); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Drain the request so the server observes cancellation of its response.
		r.ParseMultipartForm(1 << 20)
		close(entered)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	engine.stt.endpoint = server.URL
	stopped := engine.ToggleAsync()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("upload did not start")
	}
	closed := make(chan struct{})
	go func() { engine.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not cancel transcription")
	}
	if err := <-stopped; err != nil {
		t.Fatal("cancelled shutdown reported as transcription failure", err)
	}
	if status := engine.Status(); status.State != "idle" || status.LastAttemptFailed {
		t.Fatal("engine did not drain", status)
	}
	lock, err := acquireEngineLock()
	if err != nil {
		t.Fatal("shutdown left engine ownership behind", err)
	}
	lock.Close()
}
