#include "local_native.h"

#include <transcribe.h>
#include <transcribe/parakeet.h>

#include <atomic>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <new>
#include <string>
#include <vector>

#if defined(__GLIBC__)
#include <malloc.h>
#endif

struct vt_asr_model {
    transcribe_model *model;
    std::string model_id;
    bool supports_streaming;
    bool supports_vocabulary;
};

struct vt_asr_session {
    transcribe_session *session;
    vt_asr_model *model;
    std::atomic<bool> aborted{false};
};

namespace {

void clear_error(char *error, size_t capacity) {
    if (error != nullptr && capacity > 0) {
        error[0] = '\0';
    }
}

void set_error(char *error, size_t capacity, const char *message) {
    if (error != nullptr && capacity > 0) {
        std::snprintf(error, capacity, "%s", message == nullptr ? "native transcription failed" : message);
    }
}

void set_status_error(char *error, size_t capacity, const char *action,
                      transcribe_status status) {
    if (error != nullptr && capacity > 0) {
        std::snprintf(error, capacity, "%s: %s", action,
                      transcribe_status_string(static_cast<int>(status)));
    }
}

char *copy_string(const char *text) {
    if (text == nullptr) {
        text = "";
    }
    const size_t size = std::strlen(text) + 1;
    auto *copy = static_cast<char *>(std::malloc(size));
    if (copy != nullptr) {
        std::memcpy(copy, text, size);
    }
    return copy;
}

bool should_abort(void *user_data) {
    auto *aborted = static_cast<std::atomic<bool> *>(user_data);
    return aborted != nullptr && aborted->load(std::memory_order_relaxed);
}

// Return freed session arenas to the operating system after inference ends.
// This runs on the existing session-teardown path, outside engine locks and
// never during capture. Other allocators keep their default behavior.
void reclaim_allocator_memory() {
#if defined(__GLIBC__)
    malloc_trim(0);
#endif
}

std::vector<std::string> parse_vocabulary(const char *vocabulary) {
    std::vector<std::string> terms;
    if (vocabulary == nullptr || *vocabulary == '\0') {
        return terms;
    }
    const std::string input(vocabulary);
    size_t start = 0;
    while (start <= input.size()) {
        const size_t end = input.find('\n', start);
        const size_t length = (end == std::string::npos ? input.size() : end) - start;
        if (length > 0) {
            terms.emplace_back(input.substr(start, length));
        }
        if (end == std::string::npos) {
            break;
        }
        start = end + 1;
    }
    return terms;
}

std::vector<const char *> vocabulary_pointers(const std::vector<std::string> &terms) {
    std::vector<const char *> pointers;
    pointers.reserve(terms.size());
    for (const auto &term : terms) {
        pointers.push_back(term.c_str());
    }
    return pointers;
}

void fill_run_params(const vt_asr_model *model, const char *vocabulary,
                     transcribe_run_params *params,
                     std::vector<std::string> *terms,
                     std::vector<const char *> *pointers) {
    transcribe_run_params_init(params);
    if (model->model_id == "parakeet-unified-en") {
        params->language = "en";
    }
    if (model->supports_vocabulary) {
        *terms = parse_vocabulary(vocabulary);
        *pointers = vocabulary_pointers(*terms);
        params->vocabulary = pointers->empty() ? nullptr : pointers->data();
        params->n_vocabulary = static_cast<int32_t>(pointers->size());
    }
}

} // namespace

extern "C" {

int vt_asr_model_open(const char *path, const char *model_id,
                      vt_asr_model **out, char *backend,
                      size_t backend_size, int *supports_streaming,
                      int *supports_vocabulary, char *error,
                      size_t error_size) {
    clear_error(error, error_size);
    if (out != nullptr) {
        *out = nullptr;
    }
    if (backend != nullptr && backend_size > 0) {
        backend[0] = '\0';
    }
    if (supports_streaming != nullptr) {
        *supports_streaming = 0;
    }
    if (supports_vocabulary != nullptr) {
        *supports_vocabulary = 0;
    }
    if (path == nullptr || model_id == nullptr || out == nullptr) {
        set_error(error, error_size, "model path and identifier are required");
        return TRANSCRIBE_ERR_INVALID_ARG;
    }
    try {
        const transcribe_status init_status = transcribe_init_backends_default();
        if (init_status != TRANSCRIBE_OK &&
            !transcribe_backend_available(TRANSCRIBE_BACKEND_CPU)) {
            set_status_error(error, error_size, "initialize inference backends", init_status);
            return init_status;
        }

        transcribe_model_load_params params;
        transcribe_model_load_params_init(&params);
        params.backend = TRANSCRIBE_BACKEND_AUTO;
        transcribe_model *loaded = nullptr;
        transcribe_status status = transcribe_model_load_file(path, &params, &loaded);
        if (status != TRANSCRIBE_OK) {
            params.backend = TRANSCRIBE_BACKEND_CPU;
            status = transcribe_model_load_file(path, &params, &loaded);
        }
        if (status != TRANSCRIBE_OK || loaded == nullptr) {
            set_status_error(error, error_size, "load local model", status);
            return status == TRANSCRIBE_OK ? TRANSCRIBE_ERR_BACKEND : status;
        }

        transcribe_capabilities capabilities;
        transcribe_capabilities_init(&capabilities);
        status = transcribe_model_get_capabilities(loaded, &capabilities);
        if (status != TRANSCRIBE_OK) {
            transcribe_model_free(loaded);
            set_status_error(error, error_size, "read local model capabilities", status);
            return status;
        }

        auto *wrapper = new (std::nothrow) vt_asr_model{
            loaded,
            std::string(model_id),
            capabilities.supports_streaming,
            transcribe_model_supports(loaded, TRANSCRIBE_FEATURE_VOCABULARY),
        };
        if (wrapper == nullptr) {
            transcribe_model_free(loaded);
            set_error(error, error_size, "not enough memory to open local model");
            return TRANSCRIBE_ERR_OOM;
        }
        if (backend != nullptr && backend_size > 0) {
            const char *kind = transcribe_model_backend(loaded);
            std::snprintf(backend, backend_size, "%s", kind == nullptr ? "" : kind);
        }
        if (supports_streaming != nullptr) {
            *supports_streaming = wrapper->supports_streaming ? 1 : 0;
        }
        if (supports_vocabulary != nullptr) {
            *supports_vocabulary = wrapper->supports_vocabulary ? 1 : 0;
        }
        *out = wrapper;
        return TRANSCRIBE_OK;
    } catch (const std::bad_alloc &) {
        set_error(error, error_size, "not enough memory to open local model");
        return TRANSCRIBE_ERR_OOM;
    } catch (...) {
        set_error(error, error_size, "unexpected error while opening local model");
        return TRANSCRIBE_ERR_BACKEND;
    }
}

void vt_asr_model_close(vt_asr_model *model) {
    if (model == nullptr) {
        return;
    }
    transcribe_model_free(model->model);
    delete model;
}

int vt_asr_session_open(vt_asr_model *model, vt_asr_session **out,
                        char *error, size_t error_size) {
    clear_error(error, error_size);
    if (out != nullptr) {
        *out = nullptr;
    }
    if (model == nullptr || out == nullptr) {
        set_error(error, error_size, "local model is not loaded");
        return TRANSCRIBE_ERR_INVALID_ARG;
    }
    try {
        auto *wrapper = new (std::nothrow) vt_asr_session();
        if (wrapper == nullptr) {
            set_error(error, error_size, "not enough memory to create transcription session");
            return TRANSCRIBE_ERR_OOM;
        }
        const transcribe_status status = transcribe_session_init(model->model, nullptr, &wrapper->session);
        if (status != TRANSCRIBE_OK || wrapper->session == nullptr) {
            if (wrapper->session != nullptr) {
                transcribe_session_free(wrapper->session);
            }
            delete wrapper;
            set_status_error(error, error_size, "create transcription session", status);
            return status == TRANSCRIBE_OK ? TRANSCRIBE_ERR_BACKEND : status;
        }
        wrapper->model = model;
        transcribe_set_abort_callback(wrapper->session, should_abort, &wrapper->aborted);
        *out = wrapper;
        return TRANSCRIBE_OK;
    } catch (const std::bad_alloc &) {
        set_error(error, error_size, "not enough memory to create transcription session");
        return TRANSCRIBE_ERR_OOM;
    } catch (...) {
        set_error(error, error_size, "unexpected error while creating transcription session");
        return TRANSCRIBE_ERR_BACKEND;
    }
}

void vt_asr_session_close(vt_asr_session *session) {
    if (session == nullptr) {
        return;
    }
    transcribe_session_free(session->session);
    delete session;
    reclaim_allocator_memory();
}

void vt_asr_session_abort(vt_asr_session *session) {
    if (session != nullptr) {
        session->aborted.store(true, std::memory_order_relaxed);
    }
}

int vt_asr_run(vt_asr_session *session, const float *samples, int sample_count,
               const char *vocabulary, char **text, char *error,
               size_t error_size) {
    clear_error(error, error_size);
    if (text != nullptr) {
        *text = nullptr;
    }
    if (session == nullptr || samples == nullptr || sample_count <= 0 || text == nullptr) {
        set_error(error, error_size, "transcription input is empty or invalid");
        return TRANSCRIBE_ERR_INVALID_ARG;
    }
    try {
        transcribe_run_params params;
        std::vector<std::string> terms;
        std::vector<const char *> pointers;
        fill_run_params(session->model, vocabulary, &params, &terms, &pointers);
        const transcribe_status status = transcribe_run(session->session, samples, sample_count, &params);
        if (status != TRANSCRIBE_OK) {
            set_status_error(error, error_size, "transcribe audio", status);
            return status;
        }
        *text = copy_string(transcribe_full_text(session->session));
        if (*text == nullptr) {
            set_error(error, error_size, "not enough memory to copy transcription");
            return TRANSCRIBE_ERR_OOM;
        }
        return TRANSCRIBE_OK;
    } catch (const std::bad_alloc &) {
        set_error(error, error_size, "not enough memory to transcribe audio");
        return TRANSCRIBE_ERR_OOM;
    } catch (...) {
        set_error(error, error_size, "unexpected error while transcribing audio");
        return TRANSCRIBE_ERR_BACKEND;
    }
}

int vt_asr_stream_begin(vt_asr_session *session, const char *vocabulary,
                        char *error, size_t error_size) {
    clear_error(error, error_size);
    if (session == nullptr || session->session == nullptr) {
        set_error(error, error_size, "transcription session is not open");
        return TRANSCRIBE_ERR_INVALID_ARG;
    }
    try {
        if (!session->model->supports_streaming) {
            set_error(error, error_size, "this local model does not support streaming");
            return TRANSCRIBE_ERR_UNSUPPORTED_TASK;
        }
        transcribe_run_params run_params;
        std::vector<std::string> terms;
        std::vector<const char *> pointers;
        fill_run_params(session->model, vocabulary, &run_params, &terms, &pointers);
        transcribe_stream_params stream_params;
        transcribe_stream_params_init(&stream_params);
        stream_params.commit_policy = TRANSCRIBE_STREAM_COMMIT_STABLE_PREFIX;
        stream_params.stable_prefix_agreement_n = 3;
        const transcribe_status status = transcribe_stream_begin(session->session, &run_params, &stream_params);
        if (status != TRANSCRIBE_OK) {
            set_status_error(error, error_size, "start streaming transcription", status);
            return status;
        }
        return TRANSCRIBE_OK;
    } catch (const std::bad_alloc &) {
        set_error(error, error_size, "not enough memory to start streaming transcription");
        return TRANSCRIBE_ERR_OOM;
    } catch (...) {
        set_error(error, error_size, "unexpected error while starting streaming transcription");
        return TRANSCRIBE_ERR_BACKEND;
    }
}

int vt_asr_stream_feed(vt_asr_session *session, const float *samples,
                       int sample_count, int *result_changed, char *error,
                       size_t error_size) {
    clear_error(error, error_size);
    if (result_changed != nullptr) {
        *result_changed = 0;
    }
    if (session == nullptr || samples == nullptr || sample_count <= 0) {
        set_error(error, error_size, "streaming audio input is empty or invalid");
        return TRANSCRIBE_ERR_INVALID_ARG;
    }
    try {
        transcribe_stream_update update;
        transcribe_stream_update_init(&update);
        const transcribe_status status = transcribe_stream_feed(session->session, samples, sample_count, &update);
        if (status != TRANSCRIBE_OK) {
            set_status_error(error, error_size, "process streaming audio", status);
        } else if (result_changed != nullptr) {
            *result_changed = update.result_changed ? 1 : 0;
        }
        return status;
    } catch (const std::bad_alloc &) {
        set_error(error, error_size, "not enough memory to process streaming audio");
        return TRANSCRIBE_ERR_OOM;
    } catch (...) {
        set_error(error, error_size, "unexpected error while processing streaming audio");
        return TRANSCRIBE_ERR_BACKEND;
    }
}

int vt_asr_stream_finalize(vt_asr_session *session, int *result_changed,
                           char *error, size_t error_size) {
    clear_error(error, error_size);
    if (result_changed != nullptr) {
        *result_changed = 0;
    }
    if (session == nullptr) {
        set_error(error, error_size, "transcription session is not open");
        return TRANSCRIBE_ERR_INVALID_ARG;
    }
    try {
        transcribe_stream_update update;
        transcribe_stream_update_init(&update);
        const transcribe_status status = transcribe_stream_finalize(session->session, &update);
        if (status != TRANSCRIBE_OK) {
            set_status_error(error, error_size, "finish streaming transcription", status);
        } else if (result_changed != nullptr) {
            *result_changed = update.result_changed ? 1 : 0;
        }
        return status;
    } catch (const std::bad_alloc &) {
        set_error(error, error_size, "not enough memory to finish streaming transcription");
        return TRANSCRIBE_ERR_OOM;
    } catch (...) {
        set_error(error, error_size, "unexpected error while finishing streaming transcription");
        return TRANSCRIBE_ERR_BACKEND;
    }
}

int vt_asr_stream_text(vt_asr_session *session, char **full_text,
                       char **committed_text, char *error,
                       size_t error_size) {
    clear_error(error, error_size);
    if (full_text != nullptr) {
        *full_text = nullptr;
    }
    if (committed_text != nullptr) {
        *committed_text = nullptr;
    }
    if (session == nullptr || full_text == nullptr || committed_text == nullptr) {
        set_error(error, error_size, "transcription stream is not open");
        return TRANSCRIBE_ERR_INVALID_ARG;
    }
    try {
        transcribe_stream_text snapshot;
        transcribe_stream_text_init(&snapshot);
        const transcribe_status status = transcribe_stream_get_text(session->session, &snapshot);
        if (status != TRANSCRIBE_OK) {
            set_status_error(error, error_size, "read streaming transcript", status);
            return status;
        }
        *full_text = copy_string(snapshot.full_text);
        *committed_text = copy_string(snapshot.committed_text);
        if (*full_text == nullptr || *committed_text == nullptr) {
            std::free(*full_text);
            std::free(*committed_text);
            *full_text = nullptr;
            *committed_text = nullptr;
            set_error(error, error_size, "not enough memory to copy streaming transcript");
            return TRANSCRIBE_ERR_OOM;
        }
        return TRANSCRIBE_OK;
    } catch (const std::bad_alloc &) {
        set_error(error, error_size, "not enough memory to read streaming transcript");
        return TRANSCRIBE_ERR_OOM;
    } catch (...) {
        set_error(error, error_size, "unexpected error while reading streaming transcript");
        return TRANSCRIBE_ERR_BACKEND;
    }
}

void vt_asr_string_free(char *text) { std::free(text); }

} // extern "C"
