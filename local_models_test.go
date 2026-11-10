package voicetype

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPinnedLocalCatalogMetadataAndCapabilities(t *testing.T) {
	if len(localModelCatalog) != 2 {
		t.Fatalf("catalog offers %d models, want only the two pinned Parakeet models", len(localModelCatalog))
	}
	unified, ok := localModelByID(localModelUnifiedEnglish)
	if !ok || unified.Revision != "7e948f21b7bdbac698d3318db9d350f1096f3b6c" || unified.Bytes != 731_357_568 || unified.SHA256 != "4b50b6dd862bf6e346929aaf4f5eaacec003bfa3f56462d6c874b41ef2f38795" || !unified.SupportsStreaming || !strings.Contains(unified.License, "CC BY 4.0") || !strings.Contains(unified.ModelCardURL, unified.Revision) {
		t.Fatalf("Unified English artifact or streaming capability drifted: %+v", unified)
	}
	v3, ok := localModelByID(localModelParakeetV3)
	if !ok || v3.Revision != "85ac09ea12fc4b1112fa76810059364bc6adc9de" || v3.Bytes != 739_508_576 || v3.SHA256 != "5859f77944efcd8eafa23a6350731960b2b55b2203df51f319665c807d802cc7" || v3.SupportsStreaming {
		t.Fatalf("Parakeet v3 artifact or batch-only capability drifted: %+v", v3)
	}
	for _, mode := range []streamingMode{streamingModeProcess, streamingModeLive, streamingModeNone} {
		if err := validateStreamingMode(localModelUnifiedEnglish, mode); err != nil {
			t.Fatalf("Unified English rejected %q: %v", mode, err)
		}
	}
	if err := validateStreamingMode(localModelParakeetV3, streamingModeNone); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []streamingMode{streamingModeProcess, streamingModeLive} {
		if err := validateStreamingMode(localModelParakeetV3, mode); err == nil {
			t.Fatalf("Parakeet v3 unexpectedly accepts %q", mode)
		}
	}
}

func TestLocalModelDownloadPauseResumeChecksumAndRemove(t *testing.T) {
	data := bytes.Repeat([]byte("verified-local-model\n"), 8192)
	digest := sha256.Sum256(data)
	definition := localModelDefinition{
		ID:         "fixture-model",
		Name:       "Fixture model",
		Filename:   "fixture.gguf",
		Repository: "fixture/repository",
		Revision:   "fixture-revision",
		Bytes:      int64(len(data)),
		SHA256:     hex.EncodeToString(digest[:]),
		Languages:  "English",
	}
	const etag = `"fixture-etag"`
	split := 128 * 1024
	resumeHeaders := make(chan http.Header, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", etag)
		w.Header().Set("Accept-Ranges", "bytes")
		if r.Header.Get("Range") == "" {
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data[:split])
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			<-r.Context().Done()
			return
		}
		resumeHeaders <- r.Header.Clone()
		if r.Header.Get("If-Range") != etag {
			http.Error(w, "wrong validator", http.StatusPreconditionFailed)
			return
		}
		startText := strings.TrimPrefix(r.Header.Get("Range"), "bytes=")
		startText = strings.TrimSuffix(startText, "-")
		start, err := strconv.Atoi(startText)
		if err != nil || start <= 0 || start >= len(data) {
			http.Error(w, "wrong range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(data)-1, len(data)))
		w.Header().Set("Content-Length", strconv.Itoa(len(data)-start))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[start:])
	}))
	defer server.Close()

	manager := newLocalModelManagerAt(t.TempDir())
	manager.catalog = []localModelDefinition{definition}
	manager.baseURL = server.URL
	manager.client = server.Client()
	manager.retryDelay = 0
	if err := manager.Install(definition.ID); err != nil {
		t.Fatal(err)
	}
	job := manager.jobForTest(t, definition.ID)
	waitForModelProgress(t, manager, definition.ID, int64(split))
	if err := manager.Pause(definition.ID); err != nil {
		t.Fatal(err)
	}
	<-job.done
	paused := manager.statusForTest(definition.ID)
	if paused.State != localModelPaused || paused.DownloadedBytes == 0 {
		t.Fatalf("partial download was not preserved as paused: %+v", paused)
	}
	if err := manager.Resume(definition.ID); err != nil {
		t.Fatal(err)
	}
	waitForModelDownload(t, manager, definition.ID)
	ready := manager.statusForTest(definition.ID)
	if ready.State != localModelReady || ready.DownloadedBytes != definition.Bytes || ready.Progress != 100 {
		t.Fatalf("resumed download did not become ready: %+v", ready)
	}
	select {
	case headers := <-resumeHeaders:
		if headers.Get("If-Range") != etag || !strings.HasPrefix(headers.Get("Range"), "bytes=") {
			t.Fatalf("resume omitted artifact validator/range: %v", headers)
		}
	case <-time.After(time.Second):
		t.Fatal("resume request did not reach the server")
	}
	final, partial, metadata, receipt, err := manager.filePaths(definition.ID)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := os.ReadFile(final)
	if err != nil || !bytes.Equal(installed, data) {
		t.Fatalf("installed bytes differ from verified fixture: %v", err)
	}
	info, err := os.Stat(final)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("model file is not private: info=%v err=%v", info, err)
	}
	for _, directory := range []string{filepath.Dir(final), filepath.Dir(filepath.Dir(final))} {
		info, err := os.Stat(directory)
		if err != nil || info.Mode().Perm() != 0700 {
			t.Fatalf("model directory is not private: path=%s info=%v err=%v", directory, info, err)
		}
	}
	for _, path := range []string{partial, metadata} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("temporary download file remains at %s: %v", path, err)
		}
	}
	if info, err := os.Stat(receipt); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private verified receipt was not written: info=%v err=%v", info, err)
	}
	if err := manager.Remove(definition.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(final); !os.IsNotExist(err) {
		t.Fatalf("removed model still exists: %v", err)
	}
}

func TestLocalModelCancelRemovesPartialAndBadChecksumNeverBecomesReady(t *testing.T) {
	data := []byte("not-the-cataloged-content")
	definition := localModelDefinition{
		ID:         "fixture-model",
		Name:       "Fixture model",
		Filename:   "fixture.gguf",
		Repository: "fixture/repository",
		Revision:   "fixture-revision",
		Bytes:      int64(len(data)),
		SHA256:     strings.Repeat("0", 64),
	}
	requestStarted := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"bad-hash-fixture"`)
		close(requestStarted)
		_, _ = io.WriteString(w, string(data))
	}))
	defer server.Close()
	manager := newLocalModelManagerAt(t.TempDir())
	manager.catalog = []localModelDefinition{definition}
	manager.baseURL = server.URL
	manager.client = server.Client()
	manager.retryDelay = 0
	if err := manager.Install(definition.ID); err != nil {
		t.Fatal(err)
	}
	job := manager.jobForTest(t, definition.ID)
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("download request did not start")
	}
	<-job.done
	failed := manager.statusForTest(definition.ID)
	if failed.State != localModelFailed || !strings.Contains(failed.Error, "SHA-256") {
		t.Fatalf("bad checksum was not rejected: %+v", failed)
	}
	final, partial, metadata, receipt, err := manager.filePaths(definition.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{final, receipt} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("unverified model was published at %s: %v", path, err)
		}
	}
	if err := manager.Cancel(definition.ID); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{partial, metadata} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("cancel retained partial file %s: %v", path, err)
		}
	}
	if got := manager.statusForTest(definition.ID).State; got != localModelNotInstalled {
		t.Fatalf("cancel state = %s, want %s", got, localModelNotInstalled)
	}
}

func TestCancelRunningLocalModelDownloadRemovesPartial(t *testing.T) {
	data := bytes.Repeat([]byte("cancel me\n"), 4096)
	digest := sha256.Sum256(data)
	definition := localModelDefinition{
		ID:         "fixture-model",
		Name:       "Fixture model",
		Filename:   "fixture.gguf",
		Repository: "fixture/repository",
		Revision:   "fixture-revision",
		Bytes:      int64(len(data)),
		SHA256:     hex.EncodeToString(digest[:]),
	}
	const block = 16 * 1024
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"cancel-fixture"`)
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data[:block])
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	manager := newLocalModelManagerAt(t.TempDir())
	manager.catalog = []localModelDefinition{definition}
	manager.baseURL = server.URL
	manager.client = server.Client()
	manager.retryDelay = 0
	if err := manager.Install(definition.ID); err != nil {
		t.Fatal(err)
	}
	job := manager.jobForTest(t, definition.ID)
	waitForModelProgress(t, manager, definition.ID, block)
	if err := manager.Cancel(definition.ID); err != nil {
		t.Fatal(err)
	}
	<-job.done
	final, partial, metadata, receipt, err := manager.filePaths(definition.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{final, partial, metadata, receipt} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("cancelled download left file %s: %v", path, err)
		}
	}
	if got := manager.statusForTest(definition.ID).State; got != localModelNotInstalled {
		t.Fatalf("cancel state = %s, want %s", got, localModelNotInstalled)
	}
}

func TestRetryLocalModelAfterTransientHTTPFailure(t *testing.T) {
	data := []byte("retryable local model")
	digest := sha256.Sum256(data)
	definition := localModelDefinition{
		ID:         "fixture-model",
		Name:       "Fixture model",
		Filename:   "fixture.gguf",
		Repository: "fixture/repository",
		Revision:   "fixture-revision",
		Bytes:      int64(len(data)),
		SHA256:     hex.EncodeToString(digest[:]),
	}
	serveModel := atomic.Bool{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !serveModel.Load() {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("ETag", `"retry-fixture"`)
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		_, _ = w.Write(data)
	}))
	defer server.Close()
	manager := newLocalModelManagerAt(t.TempDir())
	manager.catalog = []localModelDefinition{definition}
	manager.baseURL = server.URL
	manager.client = server.Client()
	manager.retryDelay = 0
	if err := manager.Install(definition.ID); err != nil {
		t.Fatal(err)
	}
	waitForModelDownload(t, manager, definition.ID)
	if failed := manager.statusForTest(definition.ID); failed.State != localModelFailed {
		t.Fatalf("transient failures did not enter retryable failed state: %+v", failed)
	}
	serveModel.Store(true)
	if err := manager.Retry(definition.ID); err != nil {
		t.Fatal(err)
	}
	waitForModelDownload(t, manager, definition.ID)
	if ready := manager.statusForTest(definition.ID); ready.State != localModelReady {
		t.Fatalf("explicit retry did not install model: %+v", ready)
	}
}

func TestLocalModelResumeRestartsAfterETagChanges(t *testing.T) {
	data := bytes.Repeat([]byte("pinned"), 4096)
	altered := bytes.Repeat([]byte("stale!"), 4096)
	digest := sha256.Sum256(data)
	definition := localModelDefinition{
		ID:         "fixture-model",
		Name:       "Fixture model",
		Filename:   "fixture.gguf",
		Repository: "fixture/repository",
		Revision:   "fixture-revision",
		Bytes:      int64(len(data)),
		SHA256:     hex.EncodeToString(digest[:]),
	}
	const oldETag = `"old-artifact"`
	const newETag = `"new-artifact"`
	const split = 1024
	var requestCount atomic.Int32
	requestHeaders := make(chan http.Header, 3)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestHeaders <- r.Header.Clone()
		switch requestCount.Add(1) {
		case 1:
			w.Header().Set("ETag", oldETag)
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data[:split])
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			<-r.Context().Done()
		case 2:
			w.Header().Set("ETag", newETag)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", split, len(data)-1, len(data)))
			w.Header().Set("Content-Length", strconv.Itoa(len(altered)-split))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(altered[split:])
		default:
			w.Header().Set("ETag", newETag)
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
		}
	}))
	defer server.Close()
	manager := newLocalModelManagerAt(t.TempDir())
	manager.catalog = []localModelDefinition{definition}
	manager.baseURL = server.URL
	manager.client = server.Client()
	manager.retryDelay = 0
	if err := manager.Install(definition.ID); err != nil {
		t.Fatal(err)
	}
	job := manager.jobForTest(t, definition.ID)
	waitForModelProgress(t, manager, definition.ID, split)
	if err := manager.Pause(definition.ID); err != nil {
		t.Fatal(err)
	}
	<-job.done
	if err := manager.Resume(definition.ID); err != nil {
		t.Fatal(err)
	}
	waitForModelDownload(t, manager, definition.ID)
	if status := manager.statusForTest(definition.ID); status.State != localModelReady {
		t.Fatalf("download did not restart from the pinned artifact after ETag changed: %+v", status)
	}
	if got := requestCount.Load(); got != 3 {
		t.Fatalf("got %d requests, want initial, rejected partial, and clean restart", got)
	}
	first, second, third := <-requestHeaders, <-requestHeaders, <-requestHeaders
	if first.Get("Range") != "" || second.Get("If-Range") != oldETag || !strings.HasPrefix(second.Get("Range"), "bytes=") || third.Get("Range") != "" {
		t.Fatalf("unexpected resume/restart headers: first=%v second=%v third=%v", first, second, third)
	}
	final, _, _, _, err := manager.filePaths(definition.ID)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := os.ReadFile(final)
	if err != nil || !bytes.Equal(installed, data) {
		t.Fatalf("installed model included unconfirmed resumed bytes: %v", err)
	}
}

func TestLocalModelManagerClosePausesDownloadForResume(t *testing.T) {
	data := bytes.Repeat([]byte("shutdown-resume\n"), 4096)
	digest := sha256.Sum256(data)
	definition := localModelDefinition{
		ID:         "fixture-model",
		Name:       "Fixture model",
		Filename:   "fixture.gguf",
		Repository: "fixture/repository",
		Revision:   "fixture-revision",
		Bytes:      int64(len(data)),
		SHA256:     hex.EncodeToString(digest[:]),
	}
	const etag = `"shutdown-fixture"`
	const split = 16 * 1024
	var requestCount atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestCount.Add(1) == 1 {
			w.Header().Set("ETag", etag)
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data[:split])
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			<-r.Context().Done()
			return
		}
		startText := strings.TrimSuffix(strings.TrimPrefix(r.Header.Get("Range"), "bytes="), "-")
		start, err := strconv.Atoi(startText)
		if err != nil || start < split || r.Header.Get("If-Range") != etag {
			http.Error(w, "resume validator mismatch", http.StatusPreconditionFailed)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(data)-1, len(data)))
		w.Header().Set("Content-Length", strconv.Itoa(len(data)-start))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[start:])
	}))
	defer server.Close()
	root := t.TempDir()
	manager := newLocalModelManagerAt(root)
	manager.catalog = []localModelDefinition{definition}
	manager.baseURL = server.URL
	manager.client = server.Client()
	if err := manager.Install(definition.ID); err != nil {
		t.Fatal(err)
	}
	waitForModelProgress(t, manager, definition.ID, split)
	manager.Close()
	manager.Close()
	if status := manager.statusForTest(definition.ID); status.State != localModelPaused || status.DownloadedBytes != split {
		t.Fatalf("shutdown did not leave a resumable partial download: %+v", status)
	}
	resumed := newLocalModelManagerAt(root)
	resumed.catalog = []localModelDefinition{definition}
	resumed.baseURL = server.URL
	resumed.client = server.Client()
	defer resumed.Close()
	if err := resumed.Resume(definition.ID); err != nil {
		t.Fatal(err)
	}
	waitForModelDownload(t, resumed, definition.ID)
	if status := resumed.statusForTest(definition.ID); status.State != localModelReady {
		t.Fatalf("resumed download did not become ready after restart: %+v", status)
	}
	final, _, _, _, err := resumed.filePaths(definition.ID)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := os.ReadFile(final)
	if err != nil || !bytes.Equal(installed, data) {
		t.Fatalf("resumed model differs from pinned bytes: %v", err)
	}
}

func TestLocalModelCannotBeRemovedWhileInUse(t *testing.T) {
	manager := newLocalModelManagerAt(t.TempDir())
	definition := localModelCatalog[0]
	final, _, _, _, err := manager.filePaths(definition.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(final, []byte("model fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	manager.active[definition.ID] = 1
	manager.uses.Add(1)
	manager.mu.Unlock()
	if err := manager.Remove(definition.ID); err == nil || !strings.Contains(err.Error(), "current dictation") {
		t.Fatalf("in-use model removal was not rejected: %v", err)
	}
	if _, err := os.Stat(final); err != nil {
		t.Fatalf("rejected removal deleted the model: %v", err)
	}
	manager.release(definition.ID)
	if err := manager.Remove(definition.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(final); !os.IsNotExist(err) {
		t.Fatalf("model removal after release failed: %v", err)
	}
}

func (m *LocalModelManager) jobForTest(t *testing.T, id string) *modelDownload {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		m.mu.Lock()
		job := m.jobs[id]
		m.mu.Unlock()
		if job != nil {
			return job
		}
		select {
		case <-deadline:
			t.Fatal("model download job was not registered")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func waitForModelDownload(t *testing.T, manager *LocalModelManager, id string) {
	waitForModelDownloadTimeout(t, manager, id, 5*time.Second)
}

func waitForModelDownloadTimeout(t *testing.T, manager *LocalModelManager, id string, timeout time.Duration) {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		manager.mu.Lock()
		job := manager.jobs[id]
		state := manager.statuses[id].State
		manager.mu.Unlock()
		if job == nil && (state == localModelReady || state == localModelFailed || state == localModelPaused || state == localModelNotInstalled) {
			return
		}
		if job != nil {
			select {
			case <-job.done:
				return
			case <-deadline.C:
				t.Fatalf("model download did not finish within %s", timeout)
			}
		}
		select {
		case <-deadline.C:
			t.Fatalf("model download did not finish within %s", timeout)
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func (m *LocalModelManager) statusForTest(id string) LocalModel {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statuses[id]
}

func waitForModelProgress(t *testing.T, manager *LocalModelManager, id string, minimum int64) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		if manager.statusForTest(id).DownloadedBytes >= minimum {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("download did not reach %d bytes: %+v", minimum, manager.statusForTest(id))
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestDownloadRetryabilityIsLimitedToTransientFailures(t *testing.T) {
	if !downloadErrorRetryable(retryableModelDownloadError{errors.New("HTTP 503")}) {
		t.Fatal("server failure was not retryable")
	}
	if downloadErrorRetryable(errors.New("checksum mismatch")) {
		t.Fatal("deterministic checksum failure should not retry")
	}
	if downloadErrorRetryable(context.Canceled) {
		t.Fatal("cancellation should not retry")
	}
}

func TestModelManagerShutdownWaitsForActiveUse(t *testing.T) {
	manager := newLocalModelManagerAt(t.TempDir())
	id := localModelUnifiedEnglish
	manager.mu.Lock()
	manager.active[id] = 1
	manager.uses.Add(1)
	manager.mu.Unlock()
	use := &localModelUse{manager: manager, id: id}
	done := make(chan struct{})
	go func() { manager.Close(); close(done) }()
	deadline := time.Now().Add(time.Second)
	for {
		manager.mu.Lock()
		closed := manager.closed
		manager.mu.Unlock()
		if closed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("shutdown did not start")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-done:
		t.Fatal("shutdown freed model before active use was released")
	default:
	}
	if _, err := manager.acquire(context.Background(), id); err == nil {
		t.Fatal("acquired model after shutdown started")
	}
	if err := manager.Install(id); err == nil {
		t.Fatal("started download after shutdown")
	}
	use.close()
	use.close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not finish after release")
	}
}
