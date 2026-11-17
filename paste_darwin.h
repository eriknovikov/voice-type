#ifndef VOICE_TYPE_PASTE_DARWIN_H
#define VOICE_TYPE_PASTE_DARWIN_H

#include <stddef.h>
#include <stdint.h>

enum vt_paste_status {
    VT_PASTE_OK,
    VT_PASTE_CHANGED,
    VT_PASTE_SNAPSHOT_ERROR,
    VT_PASTE_TOO_LARGE,
    VT_PASTE_WRITE_ERROR,
    VT_PASTE_NATIVE_ERROR
};

int vt_pasteboard_begin(const char *text, size_t length, void **out);
int vt_pasteboard_state(void *transaction, int *owned, uint64_t *reads);
int vt_pasteboard_owned(void *transaction);
int vt_pasteboard_restore(void *transaction);
void vt_pasteboard_close(void *transaction);

#endif
