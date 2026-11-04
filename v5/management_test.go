package voicetype

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagementSocketAndIsolation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	cfg := configDefaults()
	d := newDaemon(cfg, &resources{}, sttSettings{}, "")
	srv, err := d.serveManagement()
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	info, err := os.Stat(ManagementSocketPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal("socket is not private")
	}
	client := NewManagementClient()
	defer client.CloseIdleConnections()
	resp, err := client.Get("http://desktop/settings")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(data), "revision") {
		t.Fatalf("settings response: %d %s", resp.StatusCode, data)
	}
	if _, err := d.serveManagement(); err == nil {
		t.Fatal("second server replaced live socket")
	}
	req := httptest.NewRequest("GET", "http://localhost/settings", nil)
	w := httptest.NewRecorder()
	d.routes().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatal("management exposed over existing HTTP routes")
	}
}
func TestManagementRejectsUnsafeDirectoryAndPath(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	path := ManagementSocketPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	d := newDaemon(configDefaults(), &resources{}, sttSettings{}, "")
	if _, err := d.serveManagement(); err == nil {
		t.Fatal("public directory accepted")
	}
	os.Chmod(filepath.Dir(path), 0700)
	os.WriteFile(path, []byte("keep"), 0600)
	if _, err := d.serveManagement(); err == nil {
		t.Fatal("regular file overwritten")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "keep" {
		t.Fatal("existing file changed")
	}
}
func TestDesktopLogsArePersistentPrivateAndClearable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	log := newRunLog("test")
	defer log.close("ok")
	log.write(runLogRecord{Event: "dictation.completed", Outcome: "ok"})
	records, err := log.records()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Timestamp == "" {
		t.Fatal("terminal launch did not persist timestamped record")
	}
	info, _ := os.Stat(runLogFilePath())
	if info.Mode().Perm() != 0600 {
		t.Fatal("logs not private")
	}
	if err := log.clear(); err != nil {
		t.Fatal(err)
	}
	records, err = log.records()
	if err != nil || len(records) != 0 {
		t.Fatal("clear failed")
	}
	log.write(runLogRecord{Event: "dictation.completed", Outcome: "ok"})
	records, err = log.records()
	if err != nil || len(records) != 1 {
		t.Fatal("logging broken after clear")
	}
}
