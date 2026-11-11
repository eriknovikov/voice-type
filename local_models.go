package voicetype

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type localModelDefinition struct {
	ID         string
	Name       string
	Filename   string
	Repository string
	Revision   string
	Bytes      int64
	// RAMBytes is the measured resident memory added by a loaded model.
	RAMBytes          int64
	SHA256            string
	Languages         string
	SupportsStreaming bool
	VocabularySupport string
	License           string
	LicenseURL        string
	ModelCardURL      string
}

var localModelCatalog = []localModelDefinition{
	{
		ID:                localModelUnifiedEnglish,
		Name:              "Parakeet Unified English",
		Filename:          "parakeet-unified-en-0.6b-Q8_0.gguf",
		Repository:        "handy-computer/parakeet-unified-en-0.6b-gguf",
		Revision:          "7e948f21b7bdbac698d3318db9d350f1096f3b6c",
		Bytes:             731_357_568,
		RAMBytes:          1_181_116_006, // ~1.1 GiB resident while loaded
		SHA256:            "4b50b6dd862bf6e346929aaf4f5eaacec003bfa3f56462d6c874b41ef2f38795",
		Languages:         "English",
		SupportsStreaming: true,
		VocabularySupport: "Biasing",
		License:           "NVIDIA Open Model License (upstream; GGUF card also claims CC BY 4.0)",
		LicenseURL:        "https://www.nvidia.com/en-us/agreements/enterprise-software/nvidia-open-model-license/",
		ModelCardURL:      "https://huggingface.co/handy-computer/parakeet-unified-en-0.6b-gguf/blob/7e948f21b7bdbac698d3318db9d350f1096f3b6c/README.md",
	},
	{
		ID:                localModelParakeetV3,
		Name:              "Parakeet TDT v3",
		Filename:          "parakeet-tdt-0.6b-v3-Q8_0.gguf",
		Repository:        "handy-computer/parakeet-tdt-0.6b-v3-gguf",
		Revision:          "85ac09ea12fc4b1112fa76810059364bc6adc9de",
		Bytes:             739_508_576,
		RAMBytes:          1_288_490_189, // ~1.2 GiB resident while loaded
		SHA256:            "5859f77944efcd8eafa23a6350731960b2b55b2203df51f319665c807d802cc7",
		Languages:         "25 European languages",
		VocabularySupport: "Biasing",
		License:           "CC BY 4.0",
		LicenseURL:        "https://creativecommons.org/licenses/by/4.0/",
		ModelCardURL:      "https://huggingface.co/handy-computer/parakeet-tdt-0.6b-v3-gguf/blob/85ac09ea12fc4b1112fa76810059364bc6adc9de/README.md",
	},
}

const (
	localModelNotInstalled = "not_installed"
	localModelDownloading  = "downloading"
	localModelPausing      = "pausing"
	localModelPaused       = "paused"
	localModelVerifying    = "verifying"
	localModelReady        = "ready"
	localModelFailed       = "failed"
	localModelCancelling   = "cancelling"
)

type LocalModel struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	Bytes             int64   `json:"bytes"`
	RAMBytes          int64   `json:"ram_bytes"`
	DownloadedBytes   int64   `json:"downloaded_bytes"`
	Progress          float64 `json:"progress"`
	Languages         string  `json:"languages"`
	SupportsStreaming bool    `json:"supports_streaming"`
	License           string  `json:"license"`
	LicenseURL        string  `json:"license_url"`
	ModelCardURL      string  `json:"model_card_url"`
	State             string  `json:"state"`
	Error             string  `json:"error"`
}

type partialModelMetadata struct {
	Revision string `json:"revision"`
	SHA256   string `json:"sha256"`
	ETag     string `json:"etag"`
}

type installedModelReceipt struct {
	Revision     string `json:"revision"`
	SHA256       string `json:"sha256"`
	Bytes        int64  `json:"bytes"`
	ModifiedNano int64  `json:"modified_nano"`
}

type modelDownload struct {
	cancel          context.CancelFunc
	done            chan struct{}
	pauseRequested  atomic.Bool
	cancelRequested atomic.Bool
}

type LocalModelManager struct {
	// Serialize file inspection/promotion with start, cancel, removal and loading.
	// Download workers own their files until jobs is cleared; List skips them.
	opMu       sync.Mutex
	uses       sync.WaitGroup
	mu         sync.Mutex
	root       string
	catalog    []localModelDefinition
	baseURL    string
	client     *http.Client
	retryDelay time.Duration
	closed     bool
	jobs       map[string]*modelDownload
	statuses   map[string]LocalModel
	active     map[string]int
	removing   map[string]bool

	nativeMu sync.Mutex
	native   map[string]*nativeModel
}

func newLocalModelManager() *LocalModelManager {
	return &LocalModelManager{
		catalog:    append([]localModelDefinition(nil), localModelCatalog...),
		baseURL:    "https://huggingface.co",
		client:     modelHTTPClient,
		retryDelay: 500 * time.Millisecond,
		jobs:       make(map[string]*modelDownload),
		statuses:   make(map[string]LocalModel),
		active:     make(map[string]int),
		removing:   make(map[string]bool),
		native:     make(map[string]*nativeModel),
	}
}

func newLocalModelManagerAt(root string) *LocalModelManager {
	m := newLocalModelManager()
	m.root = root
	return m
}

func (m *LocalModelManager) definition(id string) (localModelDefinition, bool) {
	for _, definition := range m.catalog {
		if definition.ID == id {
			return definition, true
		}
	}
	return localModelDefinition{}, false
}

func validLocalModelID(id string) bool {
	_, ok := localModelByID(id)
	return ok
}

func localModelSupportsStreaming(id string) bool {
	model, ok := localModelByID(id)
	return ok && model.SupportsStreaming
}

func localModelByID(id string) (localModelDefinition, bool) {
	for _, model := range localModelCatalog {
		if model.ID == id {
			return model, true
		}
	}
	return localModelDefinition{}, false
}

func defaultStreamingMode(modelID string) string {
	if localModelSupportsStreaming(modelID) {
		return streamingModeProcess
	}
	return streamingModeNone
}

func validateStreamingMode(modelID string, mode streamingMode) error {
	if !validLocalModelID(modelID) {
		return errors.New("choose a supported local transcription model")
	}
	if !localModelSupportsStreaming(modelID) {
		if mode != streamingModeNone {
			return errors.New("this model does not support streaming")
		}
		return nil
	}
	switch mode {
	case streamingModeProcess, streamingModeLive, streamingModeNone:
		return nil
	default:
		return errors.New("choose a valid streaming mode")
	}
}

func modelDataDirectory() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.New("cannot locate your home directory")
		}
		base = filepath.Join(home, ".local", "share")
	}
	if !filepath.IsAbs(base) {
		return "", errors.New("XDG_DATA_HOME must be an absolute path")
	}
	return filepath.Join(base, "voice-type", "models"), nil
}

func (m *LocalModelManager) directory() (string, error) {
	m.mu.Lock()
	root := m.root
	m.mu.Unlock()
	if root == "" {
		var err error
		root, err = modelDataDirectory()
		if err != nil {
			return "", err
		}
		m.mu.Lock()
		if m.root == "" {
			m.root = root
		} else {
			root = m.root
		}
		m.mu.Unlock()
	}
	appDir := filepath.Dir(root)
	directories := []string{appDir, root}
	for _, dir := range directories {
		info, err := os.Lstat(dir)
		if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return "", errors.New("model directory must be a real directory")
		}
		if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("inspect private model directory: %w", err)
		}
	}
	for _, dir := range directories {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return "", fmt.Errorf("create private model directory: %w", err)
		}
		info, err := os.Lstat(dir)
		if err != nil {
			return "", fmt.Errorf("inspect private model directory: %w", err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("model directory must be a real directory")
		}
		if err := os.Chmod(dir, 0700); err != nil {
			return "", fmt.Errorf("protect model directory: %w", err)
		}
	}
	return root, nil
}

func (m *LocalModelManager) filePaths(id string) (final, partial, metadata, receipt string, err error) {
	model, ok := m.definition(id)
	if !ok {
		return "", "", "", "", errors.New("unknown local model")
	}
	root, err := m.directory()
	if err != nil {
		return "", "", "", "", err
	}
	base := filepath.Join(root, model.ID)
	return base + ".gguf", base + ".partial", base + ".partial.json", base + ".verified.json", nil
}

func (m *LocalModelManager) List() ([]LocalModel, error) {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return nil, errors.New("model manager is shutting down")
	}
	if _, err := m.directory(); err != nil {
		return nil, err
	}
	models := make([]LocalModel, 0, len(m.catalog))
	for _, definition := range m.catalog {
		m.mu.Lock()
		status, hasStatus := m.statuses[definition.ID]
		job := m.jobs[definition.ID]
		m.mu.Unlock()
		if job != nil {
			models = append(models, status)
			continue
		}
		if !hasStatus || status.State != localModelFailed {
			status = m.inspect(definition)
		}
		models = append(models, status)
	}
	return models, nil
}

func (m *LocalModelManager) inspect(definition localModelDefinition) LocalModel {
	status := modelStatus(definition, localModelNotInstalled, 0, "")
	final, partial, metadataPath, receiptPath, err := m.filePaths(definition.ID)
	if err != nil {
		return modelStatus(definition, localModelFailed, 0, err.Error())
	}
	if info, err := os.Lstat(final); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			status = modelStatus(definition, localModelFailed, 0, "model file is not a regular private file")
		} else if info.Size() != definition.Bytes {
			status = modelStatus(definition, localModelFailed, info.Size(), "installed model size does not match the catalog")
		} else if receipt, readErr := readModelReceipt(receiptPath); readErr == nil && receiptMatches(receipt, info, definition) {
			_ = os.Chmod(final, 0600)
			status = modelStatus(definition, localModelReady, definition.Bytes, "")
		} else {
			status = modelStatus(definition, localModelVerifying, definition.Bytes, "")
			digest, count, verifyErr := hashFile(context.Background(), final)
			if verifyErr != nil {
				status = modelStatus(definition, localModelFailed, count, "could not verify installed model: "+verifyErr.Error())
			} else if count != definition.Bytes || digest != definition.SHA256 {
				status = modelStatus(definition, localModelFailed, count, "installed model checksum does not match the catalog; remove it and download again")
			} else if err := writeModelReceipt(receiptPath, final, definition); err != nil {
				status = modelStatus(definition, localModelFailed, count, "could not save model verification: "+err.Error())
			} else {
				_ = os.Chmod(final, 0600)
				status = modelStatus(definition, localModelReady, count, "")
			}
		}
	} else if !os.IsNotExist(err) {
		status = modelStatus(definition, localModelFailed, 0, "could not inspect installed model: "+err.Error())
	} else if info, statErr := os.Lstat(partial); statErr == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			status = modelStatus(definition, localModelFailed, 0, "partial model file is not a regular private file")
		} else {
			_ = os.Chmod(partial, 0600)
			metadata, metaErr := readPartialMetadata(metadataPath)
			if metaErr == nil && !metadataMatches(metadata, definition) {
				_ = os.Remove(partial)
				_ = os.Remove(metadataPath)
				status = modelStatus(definition, localModelNotInstalled, 0, "")
			} else if info.Size() >= definition.Bytes {
				status = modelStatus(definition, localModelVerifying, info.Size(), "")
				if m.verifyAndPromote(context.Background(), definition, partial, metadataPath, final, receiptPath) == nil {
					status = modelStatus(definition, localModelReady, definition.Bytes, "")
				} else {
					status = modelStatus(definition, localModelFailed, info.Size(), "completed partial download failed checksum verification")
				}
			} else {
				status = modelStatus(definition, localModelPaused, info.Size(), "")
			}
		}
	} else if !os.IsNotExist(statErr) {
		status = modelStatus(definition, localModelFailed, 0, "could not inspect partial model: "+statErr.Error())
	}
	m.setStatus(status)
	return status
}

func modelStatus(definition localModelDefinition, state string, downloaded int64, message string) LocalModel {
	progress := float64(0)
	if definition.Bytes > 0 {
		progress = float64(downloaded) / float64(definition.Bytes) * 100
		if progress > 100 {
			progress = 100
		}
	}
	return LocalModel{
		ID:                definition.ID,
		Name:              definition.Name,
		Bytes:             definition.Bytes,
		RAMBytes:          definition.RAMBytes,
		DownloadedBytes:   downloaded,
		Progress:          progress,
		Languages:         definition.Languages,
		SupportsStreaming: definition.SupportsStreaming,
		License:           definition.License,
		LicenseURL:        definition.LicenseURL,
		ModelCardURL:      definition.ModelCardURL,
		State:             state,
		Error:             message,
	}
}

func (m *LocalModelManager) setStatus(status LocalModel) {
	m.mu.Lock()
	m.statuses[status.ID] = status
	m.mu.Unlock()
}

func (m *LocalModelManager) Install(id string) error { return m.startDownload(id) }
func (m *LocalModelManager) Resume(id string) error  { return m.startDownload(id) }
func (m *LocalModelManager) Retry(id string) error   { return m.startDownload(id) }

func (m *LocalModelManager) startDownload(id string) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return errors.New("model manager is shutting down")
	}
	m.mu.Lock()
	busy := m.jobs[id] != nil || m.removing[id] || m.active[id] > 0
	m.mu.Unlock()
	if busy {
		return errors.New("model is in use or its download is already running")
	}

	definition, ok := m.definition(id)
	if !ok {
		return errors.New("choose a supported local model")
	}
	if _, err := m.directory(); err != nil {
		return err
	}
	final, _, _, _, err := m.filePaths(id)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(final); err == nil {
		status := m.inspect(definition)
		if status.State == localModelReady {
			return errors.New("model is already installed")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return errors.New("model manager is shutting down")
	}
	if m.removing[id] || m.active[id] > 0 {
		m.mu.Unlock()
		return errors.New("model is in use")
	}
	if m.jobs[id] != nil {
		m.mu.Unlock()
		return errors.New("model download is already running")
	}
	m.mu.Unlock()
	m.nativeMu.Lock()
	if native := m.native[id]; native != nil {
		native.close()
		delete(m.native, id)
	}
	m.nativeMu.Unlock()
	m.mu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	job := &modelDownload{cancel: cancel, done: make(chan struct{})}
	m.jobs[id] = job
	m.statuses[id] = modelStatus(definition, localModelDownloading, partialSize(m.root, id), "")
	m.mu.Unlock()
	go m.runDownload(ctx, definition, job)
	return nil
}

func partialSize(root, id string) int64 {
	if root == "" {
		return 0
	}
	info, err := os.Stat(filepath.Join(root, id+".partial"))
	if err != nil {
		return 0
	}
	return info.Size()
}

func (m *LocalModelManager) Pause(id string) error {
	m.mu.Lock()
	job := m.jobs[id]
	if job == nil {
		m.mu.Unlock()
		return errors.New("model download is not running")
	}
	job.pauseRequested.Store(true)
	job.cancel()
	status := m.statuses[id]
	status.State = localModelPausing
	m.statuses[id] = status
	m.mu.Unlock()
	return nil
}

func (m *LocalModelManager) Cancel(id string) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return errors.New("model manager is shutting down")
	}
	m.mu.Lock()
	job := m.jobs[id]
	status := m.statuses[id]
	if job != nil {
		job.cancelRequested.Store(true)
		job.cancel()
		status := m.statuses[id]
		status.State = localModelCancelling
		m.statuses[id] = status
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()
	if status.State == localModelReady {
		return errors.New("remove the installed model instead of cancelling it")
	}
	_, partial, metadata, _, err := m.filePaths(id)
	if err != nil {
		return err
	}
	if err := removeModelFiles(partial, metadata); err != nil {
		return err
	}
	definition, _ := m.definition(id)
	m.inspect(definition)
	return nil
}

func (m *LocalModelManager) Remove(id string) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return errors.New("model manager is shutting down")
	}
	definition, ok := m.definition(id)
	if !ok {
		return errors.New("choose a supported local model")
	}
	m.mu.Lock()
	if m.active[id] > 0 {
		m.mu.Unlock()
		return errors.New("finish the current dictation before removing this model")
	}
	if m.jobs[id] != nil {
		m.mu.Unlock()
		return errors.New("cancel or pause the model download before removing it")
	}
	if m.removing[id] {
		m.mu.Unlock()
		return errors.New("model removal is already in progress")
	}
	m.removing[id] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.removing, id)
		m.mu.Unlock()
	}()

	m.nativeMu.Lock()
	if native := m.native[id]; native != nil {
		native.close()
		delete(m.native, id)
	}
	m.nativeMu.Unlock()
	final, partial, metadata, receipt, err := m.filePaths(id)
	if err != nil {
		return err
	}
	if err := removeModelFiles(final, partial, metadata, receipt); err != nil {
		return err
	}
	m.setStatus(modelStatus(definition, localModelNotInstalled, 0, ""))
	return nil
}

func removeModelFiles(paths ...string) error {
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (m *LocalModelManager) acquire(ctx context.Context, id string) (*localModelUse, error) {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	definition, ok := m.definition(id)
	if !ok {
		return nil, errors.New("choose a supported local model")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	if m.closed || m.removing[id] || m.jobs[id] != nil {
		m.mu.Unlock()
		return nil, errors.New("local model is downloading, shutting down or being removed")
	}
	m.active[id]++
	m.uses.Add(1)
	m.mu.Unlock()
	acquired := false
	defer func() {
		if !acquired {
			m.release(id)
		}
	}()
	final, _, _, _, err := m.filePaths(id)
	if err != nil {
		return nil, err
	}
	status := m.inspect(definition)
	if status.State != localModelReady {
		if status.State == localModelDownloading || status.State == localModelPaused || status.State == localModelVerifying {
			return nil, fmt.Errorf("%s is not ready; install it in Local Models first", definition.Name)
		}
		if status.Error != "" {
			return nil, fmt.Errorf("%s is unavailable: %s", definition.Name, status.Error)
		}
		return nil, fmt.Errorf("%s is not installed; install it in Local Models first", definition.Name)
	}

	m.nativeMu.Lock()
	native := m.native[id]
	if native == nil {
		native, err = loadNativeModel(final, id)
		if err == nil {
			m.native[id] = native
		}
	}
	m.nativeMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", definition.Name, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	acquired = true
	return &localModelUse{manager: m, id: id, native: native}, nil
}

func (m *LocalModelManager) release(id string) {
	m.mu.Lock()
	if m.active[id] > 1 {
		m.active[id]--
	} else {
		delete(m.active, id)
	}
	m.mu.Unlock()
	m.uses.Done()
}

// evictUnused closes loaded native models that the active configuration no
// longer selects. The selected model stays warm; an evicted idle model is
// loaded again on demand. Call only while no dictation uses another model.
// Eviction returns the allocator arenas promptly, so it is worth doing as soon
// as a profile switches away from local inference.
func (m *LocalModelManager) evictUnused(selected string) int {
	m.opMu.Lock()
	m.mu.Lock()
	m.nativeMu.Lock()
	evicted := make([]*nativeModel, 0, len(m.native))
	for id, native := range m.native {
		if id == selected || m.active[id] > 0 {
			continue
		}
		evicted = append(evicted, native)
		delete(m.native, id)
	}
	m.nativeMu.Unlock()
	m.mu.Unlock()
	m.opMu.Unlock()

	for _, native := range evicted {
		native.close()
	}
	return len(evicted)
}

type localModelUse struct {
	manager *LocalModelManager
	id      string
	native  *nativeModel
	once    sync.Once
}

func (u *localModelUse) close() {
	if u != nil {
		u.once.Do(func() { u.manager.release(u.id) })
	}
}

func (m *LocalModelManager) Close() {
	m.opMu.Lock()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		m.opMu.Unlock()
		return
	}
	m.closed = true
	jobs := make([]*modelDownload, 0, len(m.jobs))
	for _, job := range m.jobs {
		job.pauseRequested.Store(true)
		job.cancel()
		jobs = append(jobs, job)
	}
	m.mu.Unlock()
	m.opMu.Unlock()
	for _, job := range jobs {
		<-job.done
	}
	m.uses.Wait()
	m.nativeMu.Lock()
	for id, native := range m.native {
		native.close()
		delete(m.native, id)
	}
	m.nativeMu.Unlock()
}

func (m *LocalModelManager) runDownload(ctx context.Context, definition localModelDefinition, job *modelDownload) {
	defer close(job.done)
	defer func() {
		m.mu.Lock()
		if m.jobs[definition.ID] == job {
			delete(m.jobs, definition.ID)
		}
		m.mu.Unlock()
	}()

	final, partial, metadataPath, receiptPath, err := m.filePaths(definition.ID)
	if err != nil {
		m.finishDownload(definition, job, modelStatus(definition, localModelFailed, 0, err.Error()))
		return
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			m.finishCancelledDownload(definition, job)
			return
		}
		if err := m.downloadOnce(ctx, definition, partial, metadataPath); err != nil {
			lastErr = err
			if ctx.Err() != nil {
				m.finishCancelledDownload(definition, job)
				return
			}
			if !downloadErrorRetryable(err) || attempt == 2 {
				break
			}
			m.setDownloadProgress(definition, partial, localModelDownloading, "")
			timer := time.NewTimer(m.retryDelay * time.Duration(1<<attempt))
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				m.finishCancelledDownload(definition, job)
				return
			}
			continue
		}
		m.setDownloadProgress(definition, partial, localModelVerifying, "")
		if ctx.Err() != nil {
			m.finishCancelledDownload(definition, job)
			return
		}
		if err := m.verifyAndPromote(ctx, definition, partial, metadataPath, final, receiptPath); err != nil {
			if ctx.Err() != nil {
				m.finishCancelledDownload(definition, job)
				return
			}
			lastErr = err
			_ = removeModelFiles(partial, metadataPath)
			break
		}
		m.finishDownload(definition, job, modelStatus(definition, localModelReady, definition.Bytes, ""))
		return
	}
	message := "download failed"
	if lastErr != nil {
		message = lastErr.Error()
	}
	m.finishDownload(definition, job, modelStatus(definition, localModelFailed, partialSize(m.root, definition.ID), message))
}

func (m *LocalModelManager) finishCancelledDownload(definition localModelDefinition, job *modelDownload) {
	if job.cancelRequested.Load() {
		m.finishDownload(definition, job, modelStatus(definition, localModelNotInstalled, 0, ""))
		return
	}
	state := localModelPaused
	if job.pauseRequested.Load() {
		state = localModelPaused
	}
	_, partial, _, _, _ := m.filePaths(definition.ID)
	info, _ := os.Stat(partial)
	var bytes int64
	if info != nil {
		bytes = info.Size()
	}
	m.finishDownload(definition, job, modelStatus(definition, state, bytes, ""))
}

func (m *LocalModelManager) finishDownload(definition localModelDefinition, job *modelDownload, status LocalModel) {
	final, partial, metadata, receipt, _ := m.filePaths(definition.ID)
	m.mu.Lock()
	if m.jobs[definition.ID] != job {
		m.mu.Unlock()
		return
	}
	if !job.cancelRequested.Load() {
		m.statuses[definition.ID] = status
		delete(m.jobs, definition.ID)
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()

	cleanupErr := removeModelFiles(final, partial, metadata, receipt)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.jobs[definition.ID] != job {
		return
	}
	if cleanupErr != nil {
		status = modelStatus(definition, localModelFailed, partialSize(m.root, definition.ID), "cancelled model files could not be removed: "+cleanupErr.Error())
	} else {
		status = modelStatus(definition, localModelNotInstalled, 0, "")
	}
	m.statuses[definition.ID] = status
	delete(m.jobs, definition.ID)
}

func (m *LocalModelManager) setDownloadProgress(definition localModelDefinition, path, state, message string) {
	status := modelStatus(definition, state, partialSize(m.root, definition.ID), message)
	if path != "" {
		if info, err := os.Stat(path); err == nil {
			status = modelStatus(definition, state, info.Size(), message)
		}
	}
	m.mu.Lock()
	if job := m.jobs[definition.ID]; job != nil {
		if job.cancelRequested.Load() {
			status.State = localModelCancelling
		} else if job.pauseRequested.Load() {
			status.State = localModelPausing
		}
	}
	m.statuses[status.ID] = status
	m.mu.Unlock()
}

func (m *LocalModelManager) downloadOnce(ctx context.Context, definition localModelDefinition, partial, metadataPath string) error {
	info, statErr := os.Lstat(partial)
	if statErr != nil && !os.IsNotExist(statErr) {
		return fmt.Errorf("inspect partial download: %w", statErr)
	}
	offset := int64(0)
	metadata, _ := readPartialMetadata(metadataPath)
	if statErr == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("partial download is not a regular file")
		}
		if metadataMatches(metadata, definition) && strongETag(metadata.ETag) && info.Size() < definition.Bytes {
			offset = info.Size()
		}
	}
	if offset == definition.Bytes {
		return nil
	}
	if offset == 0 && statErr == nil {
		if err := os.Truncate(partial, 0); err != nil {
			return fmt.Errorf("restart partial download: %w", err)
		}
	}
	url := fmt.Sprintf("%s/%s/resolve/%s/%s?download=true", strings.TrimRight(m.baseURL, "/"), definition.Repository, definition.Revision, definition.Filename)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept-Encoding", "identity")
	if offset > 0 {
		request.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		request.Header.Set("If-Range", metadata.ETag)
	}
	response, err := m.client.Do(request)
	if err != nil {
		return fmt.Errorf("request model download: %w", err)
	}
	defer response.Body.Close()

	if offset > 0 && response.StatusCode == http.StatusOK {
		// If-Range did not match or the server ignored Range. This response is
		// the complete pinned artifact, so replace the partial from byte zero.
		offset = 0
		if err := os.Truncate(partial, 0); err != nil {
			return fmt.Errorf("restart partial download: %w", err)
		}
	} else if offset > 0 && response.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		info, err := os.Stat(partial)
		if err == nil && info.Size() == definition.Bytes {
			return nil
		}
		return errors.New("server rejected the partial range; retry the download")
	} else if offset > 0 {
		if response.StatusCode != http.StatusPartialContent || response.Header.Get("ETag") != metadata.ETag || !validContentRange(response.Header.Get("Content-Range"), offset, definition.Bytes) {
			// Never append bytes unless the response confirms both the exact
			// partial offset and the validator for this immutable artifact.
			if err := os.Truncate(partial, 0); err != nil {
				return fmt.Errorf("discard unconfirmed partial download: %w", err)
			}
			_ = os.Remove(metadataPath)
			_ = response.Body.Close()
			return m.downloadOnce(ctx, definition, partial, metadataPath)
		}
	} else if response.StatusCode != http.StatusOK {
		return responseStatusError(response)
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		return responseStatusError(response)
	}
	if response.ContentLength >= 0 && response.ContentLength > definition.Bytes-offset {
		return errors.New("model server advertised more bytes than the pinned artifact")
	}
	etag := response.Header.Get("ETag")
	if !strongETag(etag) {
		etag = ""
	}
	if err := writePartialMetadata(metadataPath, partialModelMetadata{Revision: definition.Revision, SHA256: definition.SHA256, ETag: etag}); err != nil {
		return fmt.Errorf("save partial download metadata: %w", err)
	}
	flags := os.O_CREATE | os.O_WRONLY
	if offset > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	file, err := os.OpenFile(partial, flags, 0600)
	if err != nil {
		return fmt.Errorf("create private partial model file: %w", err)
	}
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return fmt.Errorf("protect partial model file: %w", err)
	}
	written := offset
	buffer := make([]byte, 128*1024)
	for {
		if err := ctx.Err(); err != nil {
			_ = file.Sync()
			_ = file.Close()
			return err
		}
		count, readErr := response.Body.Read(buffer)
		if count > 0 {
			if written+int64(count) > definition.Bytes {
				_ = file.Close()
				return errors.New("download exceeds the pinned model size")
			}
			n, writeErr := file.Write(buffer[:count])
			written += int64(n)
			if writeErr != nil {
				_ = file.Close()
				return fmt.Errorf("write partial model file: %w", writeErr)
			}
			if n != count {
				_ = file.Close()
				return io.ErrShortWrite
			}
			m.setDownloadProgress(definition, partial, localModelDownloading, "")
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = file.Sync()
			_ = file.Close()
			return fmt.Errorf("read model download: %w", readErr)
		}
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync partial model file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close partial model file: %w", err)
	}
	if written != definition.Bytes {
		return fmt.Errorf("download ended at %d of %d bytes", written, definition.Bytes)
	}
	return nil
}

var modelHTTPClient = &http.Client{Timeout: 0, CheckRedirect: func(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("too many redirects while downloading model")
	}
	if len(via) > 0 && via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
		return errors.New("model download redirect downgraded from HTTPS")
	}
	return nil
}}

func responseStatusError(response *http.Response) error {
	message := strings.TrimSpace(response.Status)
	if message == "" {
		message = "HTTP request failed"
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return retryableModelDownloadError{errors.New(message)}
	}
	return errors.New(message)
}

type retryableModelDownloadError struct{ error }

func downloadErrorRetryable(err error) bool {
	var retryable retryableModelDownloadError
	if errors.As(err, &retryable) {
		return true
	}
	var transport *url.Error
	if errors.As(err, &transport) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var network net.Error
	return errors.As(err, &network)
}

func strongETag(etag string) bool {
	etag = strings.TrimSpace(etag)
	return etag != "" && !strings.HasPrefix(strings.ToUpper(etag), "W/")
}

func validContentRange(value string, offset, total int64) bool {
	if !strings.HasPrefix(value, "bytes ") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(value, "bytes "), "/")
	if len(parts) != 2 || parts[1] == "*" {
		return false
	}
	rangeParts := strings.Split(parts[0], "-")
	if len(rangeParts) != 2 {
		return false
	}
	start, startErr := strconv.ParseInt(rangeParts[0], 10, 64)
	end, endErr := strconv.ParseInt(rangeParts[1], 10, 64)
	actualTotal, totalErr := strconv.ParseInt(parts[1], 10, 64)
	return startErr == nil && endErr == nil && totalErr == nil && start == offset && end >= start && actualTotal == total
}

func (m *LocalModelManager) verifyAndPromote(ctx context.Context, definition localModelDefinition, partial, metadata, final, receipt string) error {
	if _, err := os.Stat(partial); err != nil {
		return fmt.Errorf("partial model file is missing: %w", err)
	}
	digest, size, err := hashFile(ctx, partial)
	if err != nil {
		return fmt.Errorf("verify model checksum: %w", err)
	}
	if size != definition.Bytes {
		return fmt.Errorf("model size mismatch: got %d bytes, expected %d", size, definition.Bytes)
	}
	if digest != definition.SHA256 {
		return errors.New("model SHA-256 does not match the pinned catalog")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Chmod(partial, 0600); err != nil {
		return fmt.Errorf("protect verified model: %w", err)
	}
	if err := os.Rename(partial, final); err != nil {
		return fmt.Errorf("install verified model atomically: %w", err)
	}
	if err := syncDirectory(filepath.Dir(final)); err != nil {
		return fmt.Errorf("sync model directory: %w", err)
	}
	if err := writeModelReceipt(receipt, final, definition); err != nil {
		return fmt.Errorf("save model verification receipt: %w", err)
	}
	if err := os.Remove(metadata); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove partial download metadata: %w", err)
	}
	return nil
}

func hashFile(ctx context.Context, path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	buffer := make([]byte, 1024*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return "", total, err
		}
		count, readErr := file.Read(buffer)
		if count > 0 {
			_, _ = hash.Write(buffer[:count])
			total += int64(count)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", total, readErr
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), total, nil
}

func metadataMatches(metadata partialModelMetadata, definition localModelDefinition) bool {
	return metadata.Revision == definition.Revision && metadata.SHA256 == definition.SHA256
}

func readPartialMetadata(path string) (partialModelMetadata, error) {
	var metadata partialModelMetadata
	data, err := os.ReadFile(path)
	if err != nil {
		return metadata, err
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return metadata, err
	}
	return metadata, nil
}

func writePartialMetadata(path string, metadata partialModelMetadata) error {
	data, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	return atomicPrivateWrite(path, data)
}

func readModelReceipt(path string) (installedModelReceipt, error) {
	var receipt installedModelReceipt
	data, err := os.ReadFile(path)
	if err != nil {
		return receipt, err
	}
	if err := json.Unmarshal(data, &receipt); err != nil {
		return receipt, err
	}
	return receipt, nil
}

func receiptMatches(receipt installedModelReceipt, info os.FileInfo, definition localModelDefinition) bool {
	return receipt.Revision == definition.Revision && receipt.SHA256 == definition.SHA256 &&
		receipt.Bytes == definition.Bytes && info.Size() == definition.Bytes &&
		info.ModTime().UnixNano() == receipt.ModifiedNano
}

func writeModelReceipt(path, modelPath string, definition localModelDefinition) error {
	info, err := os.Stat(modelPath)
	if err != nil {
		return err
	}
	receipt := installedModelReceipt{
		Revision:     definition.Revision,
		SHA256:       definition.SHA256,
		Bytes:        info.Size(),
		ModifiedNano: info.ModTime().UnixNano(),
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	return atomicPrivateWrite(path, data)
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
