package voicetype

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func setupServiceTest(t *testing.T) int {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
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

func TestEmbeddedServiceLifecycleAndIsolation(t *testing.T) {
	port := setupServiceTest(t)
	service, err := StartService("test")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if status := service.Status(); status.State != "idle" || status.Port != port || status.Error == "" {
		t.Fatalf("setup status: %+v", status)
	}
	if _, err := os.Stat(ManagementSocketPath()); !os.IsNotExist(err) {
		t.Fatal("embedded service created a management socket")
	}
	if _, err := StartService("second"); err == nil {
		t.Fatal("second service adopted or replaced the first")
	}
	client := &http.Client{Timeout: time.Second}
	for _, path := range []string{"/settings", "/logs", "/microphone-test", "/status", "/recovery"} {
		response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, path))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 404 {
			t.Fatalf("private route exposed: %s", path)
		}
	}
	// The real shortcut endpoint reaches this service without a child process.
	response, err := client.Post(fmt.Sprintf("http://127.0.0.1:%d/toggle", port), "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 503 {
		t.Fatalf("missing key should block capture: %d", response.StatusCode)
	}
	settings, err := ReadSettings()
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.SaveSettings(SettingsUpdate{Revision: settings.Revision, Provider: "openai", Port: port, Vocabulary: settings.Vocabulary, APIKeys: map[string]string{"openai": "test-key"}})
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
	// /exit must signal the host to quit, then release the listener.
	response, err = client.Post(fmt.Sprintf("http://127.0.0.1:%d/exit", port), "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	select {
	case <-service.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("service did not finish")
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); service.Close() }()
	}
	wg.Wait()
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal("listener leaked", err)
	}
	listener.Close()
	if _, err := os.Stat(filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "voice-type")); !os.IsNotExist(err) {
		t.Fatal("unexpected IPC state")
	}
}

func TestEmbeddedServiceShutdownDiscardsRecording(t *testing.T) {
	setupServiceTest(t)
	t.Setenv("OPENAI_API_KEY", "test-key")
	service, err := StartService("test")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	service.d.res.Recorder = &fixtureRecorder{}
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

func TestControlRejectsBrowserRequests(t *testing.T) {
	port := setupServiceTest(t)
	service, err := StartService("test")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	client := &http.Client{Timeout: time.Second}
	for _, headers := range []map[string]string{
		{"Origin": "https://example.com"},
		{"Sec-Fetch-Site": "cross-site"},
		{"Host": "attacker.example"},
	} {
		request, _ := http.NewRequest("POST", fmt.Sprintf("http://127.0.0.1:%d/exit", port), nil)
		for name, value := range headers {
			if name == "Host" {
				request.Host = value
			} else {
				request.Header.Set(name, value)
			}
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 403 {
			t.Fatalf("unsafe request accepted: %v", headers)
		}
	}
	if service.Status().State != "idle" {
		t.Fatal("browser altered service")
	}
}

func TestEmbeddedServiceStartupConflictPreservesFiles(t *testing.T) {
	port := setupServiceTest(t)
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if _, err := StartService("test"); err == nil || !strings.Contains(err.Error(), "close any other Voice Type instance") {
		t.Fatalf("conflict error: %v", err)
	}
	if _, err := os.Stat(runLogFilePath()); !os.IsNotExist(err) {
		t.Fatal("failed start created logs")
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
	setupServiceTest(t)
	t.Setenv("PULSE_SERVER", audioServer)
	service, err := StartService("test")
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
