#ifndef VOICE_TYPE_INSERT_DARWIN_H
#define VOICE_TYPE_INSERT_DARWIN_H

#include <stddef.h>
#include <stdint.h>

typedef struct vt_insert_target vt_insert_target;
typedef struct vt_insert_guard vt_insert_guard;

enum vt_insert_status {
    VT_INSERT_OK,
    VT_INSERT_CANCELLED,
    VT_INSERT_PERMISSION,
    VT_INSERT_FOCUS_CHANGED,
    VT_INSERT_UNSAFE,
    VT_INSERT_CAPABILITY_UNKNOWN,
    VT_INSERT_UNSUPPORTED,
    VT_INSERT_CAPABILITY_CHANGED,
    VT_INSERT_ALLOCATION,
    VT_INSERT_NATIVE_ERROR,
    VT_INSERT_CLIPBOARD_CHANGED
};

enum vt_insert_submission {
    VT_INSERT_UNKNOWN,
    VT_INSERT_NOT_SUBMITTED,
    VT_INSERT_SUBMITTED
};

typedef struct {
    int status;
    int submission;
    int ax_error;
} vt_insert_result;

vt_insert_guard *vt_insert_guard_create(void);
void vt_insert_guard_cancel(vt_insert_guard *guard);
void vt_insert_guard_free(vt_insert_guard *guard);
vt_insert_result vt_insert_capture(vt_insert_guard *guard, vt_insert_target **out);
vt_insert_result vt_insert_state(vt_insert_target *target, vt_insert_guard *guard, int *selected_text_writable);
vt_insert_result vt_insert_replace(vt_insert_target *target, vt_insert_guard *guard, const uint16_t *text, size_t length);
vt_insert_result vt_insert_unicode(vt_insert_target *target, vt_insert_guard *guard, const uint16_t *text, size_t length);
vt_insert_result vt_insert_paste(vt_insert_target *target, vt_insert_guard *guard, void *transaction);
void vt_insert_close(vt_insert_target *target);

#endif
