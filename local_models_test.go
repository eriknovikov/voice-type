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
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPinnedLocalCatalogMetadataAndCapabilities(t *testing.T) {
	unified, ok := localModelByID(localModelUnifiedEnglish)
	if !ok || unified.Revision != "7e948f21b7bdbac698d3318db9d350f1096f3b6c" || unified.Bytes != 731_357_568 || unified.SHA256 != "4b50b6dd862bf6e346929aaf4f5eaacec003bfa3f56462d6c874b41ef2f38795" || !unified.SupportsStreaming {
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
	if err := validateStreamingMode(localModelParakeetV3, streamingModeLive); err == nil {
		t.Fatal("Parakeet v3 unexpectedly advertises live streaming")
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
	for _, path := range []string{partial, metadata} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("temporary download file remains at %s: %v", path, err)
		}
	}
	if _, err := os.Stat(receipt); err != nil {
		t.Fatalf("verified receipt was not written: %v", err)
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
	t.Helper()
	deadline := time.After(5 * time.Second)
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
			case <-deadline:
				t.Fatal("model download did not finish")
			}
		}
		select {
		case <-deadline:
			t.Fatal("model download did not finish")
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
