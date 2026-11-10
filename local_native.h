#ifndef VOICE_TYPE_LOCAL_NATIVE_H
#define VOICE_TYPE_LOCAL_NATIVE_H

#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

struct vt_asr_model;
struct vt_asr_session;

int vt_asr_model_open(const char *path, const char *model_id,
                      struct vt_asr_model **out, char *backend,
                      size_t backend_size, int *supports_streaming,
                      int *supports_vocabulary, char *error,
                      size_t error_size);
void vt_asr_model_close(struct vt_asr_model *model);

int vt_asr_session_open(struct vt_asr_model *model,
                        struct vt_asr_session **out, char *error,
                        size_t error_size);
void vt_asr_session_close(struct vt_asr_session *session);
void vt_asr_session_abort(struct vt_asr_session *session);

int vt_asr_run(struct vt_asr_session *session, const float *samples,
               int sample_count, const char *vocabulary, char **text,
               char *error, size_t error_size);
int vt_asr_stream_begin(struct vt_asr_session *session,
                        const char *vocabulary, char *error,
                        size_t error_size);
int vt_asr_stream_feed(struct vt_asr_session *session, const float *samples,
                       int sample_count, char *error, size_t error_size);
int vt_asr_stream_finalize(struct vt_asr_session *session, char *error,
                           size_t error_size);
int vt_asr_stream_text(struct vt_asr_session *session, char **full_text,
                       char **committed_text, char *error,
                       size_t error_size);
void vt_asr_string_free(char *text);

#ifdef __cplusplus
}
#endif

#endif
