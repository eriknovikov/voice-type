#include "insert_darwin.h"
#include "paste_darwin.h"

#ifdef VT_INSERT_TEST
#include "scripts/fixtures/macos_insert_platform.h"
#else
#include <ApplicationServices/ApplicationServices.h>
#include <Carbon/Carbon.h>
#include <libproc.h>
#include <sys/proc_info.h>
#include <unistd.h>
#endif

#include <stdatomic.h>
#include <stdbool.h>
#include <limits.h>
#include <stdlib.h>

struct vt_insert_guard { atomic_bool cancelled; };
struct vt_insert_target {
    AXUIElementRef system;
    AXUIElementRef element;
    pid_t pid;
    uint64_t start_seconds;
    uint64_t start_microseconds;
};

static vt_insert_result vt_result(int status, int submission, AXError error) {
    return (vt_insert_result){status, submission, (int)error};
}

static vt_insert_result vt_before_call(vt_insert_guard *guard) {
    if (!guard || atomic_load(&guard->cancelled))
        return vt_result(VT_INSERT_CANCELLED, VT_INSERT_NOT_SUBMITTED, 0);
    if (!AXIsProcessTrusted())
        return vt_result(VT_INSERT_PERMISSION, VT_INSERT_NOT_SUBMITTED, 0);
    if (IsSecureEventInputEnabled())
        return vt_result(VT_INSERT_UNSAFE, VT_INSERT_NOT_SUBMITTED, 0);
    return vt_result(VT_INSERT_OK, VT_INSERT_NOT_SUBMITTED, 0);
}

vt_insert_guard *vt_insert_guard_create(void) {
    vt_insert_guard *guard = malloc(sizeof(*guard));
    if (guard) atomic_init(&guard->cancelled, false);
    return guard;
}

void vt_insert_guard_cancel(vt_insert_guard *guard) { atomic_store(&guard->cancelled, true); }
void vt_insert_guard_free(vt_insert_guard *guard) { free(guard); }

static int vt_process_identity(vt_insert_target *target, int capture) {
    struct proc_bsdinfo info = {0};
    if (target->pid <= 0 || target->pid == getpid() ||
        proc_pidinfo(target->pid, PROC_PIDTBSDINFO, 0, &info, sizeof(info)) != sizeof(info)) return 0;
    if (capture) {
        target->start_seconds = info.pbi_start_tvsec;
        target->start_microseconds = info.pbi_start_tvusec;
    }
    return target->start_seconds == info.pbi_start_tvsec &&
           target->start_microseconds == info.pbi_start_tvusec;
}

static AXError vt_copy_element(AXUIElementRef owner, CFStringRef attribute, AXUIElementRef *out) {
    CFTypeRef value = NULL;
    *out = NULL;
    AXError error = AXUIElementCopyAttributeValue(owner, attribute, &value);
    if (error == kAXErrorSuccess && (!value || CFGetTypeID(value) != AXUIElementGetTypeID()))
        error = kAXErrorIllegalArgument;
    if (error == kAXErrorSuccess) *out = (AXUIElementRef)value;
    else if (value) CFRelease(value);
    return error;
}

static vt_insert_result vt_check_focus(vt_insert_target *target, vt_insert_guard *guard) {
    vt_insert_result result = vt_before_call(guard);
    if (result.status != VT_INSERT_OK) return result;
    if (!target || !vt_process_identity(target, 0))
        return vt_result(VT_INSERT_FOCUS_CHANGED, VT_INSERT_NOT_SUBMITTED, 0);
    AXUIElementRef app = NULL, focused = NULL;
    AXError error = vt_copy_element(target->system, kAXFocusedApplicationAttribute, &app);
    pid_t pid = 0;
    if (error == kAXErrorSuccess) error = AXUIElementGetPid(app, &pid);
    if (app) CFRelease(app);
    if (error != kAXErrorSuccess || pid != target->pid)
        return vt_result(VT_INSERT_FOCUS_CHANGED, VT_INSERT_NOT_SUBMITTED, error);
    error = vt_copy_element(target->system, kAXFocusedUIElementAttribute, &focused);
    int matches = error == kAXErrorSuccess && CFEqual(target->element, focused);
    if (focused) CFRelease(focused);
    if (!matches) return vt_result(VT_INSERT_FOCUS_CHANGED, VT_INSERT_NOT_SUBMITTED, error);
    return vt_before_call(guard);
}

static vt_insert_result vt_editing_access(vt_insert_target *target, int *writable) {
    CFTypeRef role = NULL, subrole = NULL, enabled = NULL;
    AXError error = AXUIElementCopyAttributeValue(target->element, kAXRoleAttribute, &role);
    int text_area = error == kAXErrorSuccess && role && CFEqual(role, kAXTextAreaRole);
    int text_field = error == kAXErrorSuccess && role && CFEqual(role, kAXTextFieldRole);
    if (role) CFRelease(role);
    if (!text_area && !text_field) return vt_result(VT_INSERT_UNSAFE, VT_INSERT_NOT_SUBMITTED, error);

    error = AXUIElementCopyAttributeValue(target->element, kAXSubroleAttribute, &subrole);
    int missing = error == kAXErrorAttributeUnsupported || error == kAXErrorNoValue;
    int known_subrole = error == kAXErrorSuccess && subrole && CFGetTypeID(subrole) == CFStringGetTypeID();
    int secure = known_subrole && (CFEqual(subrole, kAXSecureTextFieldSubrole) ||
                                  CFEqual(subrole, kAXUnknownSubrole));
    if (subrole) CFRelease(subrole);
    // A text area's optional subrole can be absent. Unresolved text fields fail closed.
    if (secure || (!known_subrole && !(text_area && missing)))
        return vt_result(VT_INSERT_UNSAFE, VT_INSERT_NOT_SUBMITTED, error);

    error = AXUIElementCopyAttributeValue(target->element, kAXEnabledAttribute, &enabled);
    int editable = error == kAXErrorSuccess && enabled && CFGetTypeID(enabled) == CFBooleanGetTypeID() && CFBooleanGetValue(enabled);
    if (enabled) CFRelease(enabled);
    if (!editable) return vt_result(VT_INSERT_UNSAFE, VT_INSERT_NOT_SUBMITTED, error);

    Boolean selected_settable = false, value_settable = false;
    error = AXUIElementIsAttributeSettable(target->element, kAXSelectedTextAttribute, &selected_settable);
    if (error != kAXErrorSuccess && error != kAXErrorAttributeUnsupported)
        return vt_result(VT_INSERT_CAPABILITY_UNKNOWN, VT_INSERT_NOT_SUBMITTED, error);
    *writable = error == kAXErrorSuccess && selected_settable;
    if (!*writable) {
        // Inspect capability only. Never read or replace the document's AXValue.
        error = AXUIElementIsAttributeSettable(target->element, kAXValueAttribute, &value_settable);
        if (error != kAXErrorSuccess || !value_settable)
            return vt_result(VT_INSERT_UNSAFE, VT_INSERT_NOT_SUBMITTED, error);
    }
    return vt_result(VT_INSERT_OK, VT_INSERT_NOT_SUBMITTED, 0);
}

vt_insert_result vt_insert_state(vt_insert_target *target, vt_insert_guard *guard, int *writable) {
    *writable = 0;
    vt_insert_result result = vt_check_focus(target, guard);
    if (result.status != VT_INSERT_OK) return result;
    result = vt_editing_access(target, writable);
    if (result.status != VT_INSERT_OK) return result;
    return vt_check_focus(target, guard);
}

void vt_insert_close(vt_insert_target *target) {
    if (!target) return;
    if (target->element) CFRelease(target->element);
    if (target->system) CFRelease(target->system);
    free(target);
}

vt_insert_result vt_insert_capture(vt_insert_guard *guard, vt_insert_target **out) {
    *out = NULL;
    vt_insert_result result = vt_before_call(guard);
    if (result.status != VT_INSERT_OK) return result;
    vt_insert_target *target = calloc(1, sizeof(*target));
    if (!target) return vt_result(VT_INSERT_ALLOCATION, VT_INSERT_NOT_SUBMITTED, 0);
    target->system = AXUIElementCreateSystemWide();
    AXError error = target->system ? AXUIElementSetMessagingTimeout(target->system, 0.25f) : kAXErrorFailure;
    if (error == kAXErrorSuccess) error = vt_copy_element(target->system, kAXFocusedUIElementAttribute, &target->element);
    if (error == kAXErrorSuccess) error = AXUIElementGetPid(target->element, &target->pid);
    if (error == kAXErrorSuccess) error = AXUIElementSetMessagingTimeout(target->element, 0.25f);
    if (error != kAXErrorSuccess || !vt_process_identity(target, 1)) {
        result = vt_result(VT_INSERT_FOCUS_CHANGED, VT_INSERT_NOT_SUBMITTED, error);
    } else {
        int writable = 0;
        result = vt_insert_state(target, guard, &writable);
    }
    if (result.status != VT_INSERT_OK) vt_insert_close(target);
    else *out = target;
    return result;
}

vt_insert_result vt_insert_replace(vt_insert_target *target, vt_insert_guard *guard, const uint16_t *text, size_t length) {
    if (!text || length == 0 || length > LONG_MAX)
        return vt_result(VT_INSERT_NATIVE_ERROR, VT_INSERT_NOT_SUBMITTED, 0);
    CFStringRef replacement = CFStringCreateWithCharacters(NULL, text, (CFIndex)length);
    if (!replacement) return vt_result(VT_INSERT_ALLOCATION, VT_INSERT_NOT_SUBMITTED, 0);
    int writable = 0;
    vt_insert_result result = vt_insert_state(target, guard, &writable);
    if (result.status == VT_INSERT_OK && !writable)
        result = vt_result(VT_INSERT_UNSUPPORTED, VT_INSERT_NOT_SUBMITTED, 0);
    if (result.status == VT_INSERT_OK) {
        AXError error = AXUIElementSetAttributeValue(target->element, kAXSelectedTextAttribute, replacement);
        // Even an unsupported error from the setter follows a mutation attempt.
        result = vt_result(error == kAXErrorSuccess ? VT_INSERT_OK : VT_INSERT_NATIVE_ERROR,
                           error == kAXErrorSuccess ? VT_INSERT_SUBMITTED : VT_INSERT_UNKNOWN, error);
    }
    CFRelease(replacement);
    return result;
}

vt_insert_result vt_insert_unicode(vt_insert_target *target, vt_insert_guard *guard, const uint16_t *text, size_t length) {
    if (!text || length == 0 || length > 20)
        return vt_result(VT_INSERT_NATIVE_ERROR, VT_INSERT_NOT_SUBMITTED, 0);
    CGEventSourceRef source = CGEventSourceCreate(kCGEventSourceStatePrivate);
    CGEventRef down = source ? CGEventCreateKeyboardEvent(source, 0, true) : NULL;
    CGEventRef up = source ? CGEventCreateKeyboardEvent(source, 0, false) : NULL;
    vt_insert_result result = vt_result(VT_INSERT_ALLOCATION, VT_INSERT_NOT_SUBMITTED, 0);
    if (down && up) {
        CGEventSetFlags(down, 0);
        CGEventSetFlags(up, 0);
        CGEventKeyboardSetUnicodeString(down, length, text);
        CGEventKeyboardSetUnicodeString(up, length, text);
        int writable = 0;
        result = vt_insert_state(target, guard, &writable);
        if (result.status == VT_INSERT_OK && writable)
            result = vt_result(VT_INSERT_CAPABILITY_CHANGED, VT_INSERT_NOT_SUBMITTED, 0);
        if (result.status == VT_INSERT_OK && !CGPreflightPostEventAccess())
            result = vt_result(VT_INSERT_PERMISSION, VT_INSERT_NOT_SUBMITTED, 0);
        if (result.status == VT_INSERT_OK) result = vt_check_focus(target, guard);
        if (result.status == VT_INSERT_OK) {
            CGEventPostToPid(target->pid, down);
            CGEventPostToPid(target->pid, up); // Balance the key even if cancellation races the down event.
            result = vt_result(VT_INSERT_OK, VT_INSERT_SUBMITTED, 0);
        }
    }
    if (up) CFRelease(up);
    if (down) CFRelease(down);
    if (source) CFRelease(source);
    return result;
}

vt_insert_result vt_insert_paste(vt_insert_target *target, vt_insert_guard *guard, void *transaction) {
    CGEventSourceRef source = CGEventSourceCreate(kCGEventSourceStatePrivate);
    CGEventRef down = source ? CGEventCreateKeyboardEvent(source, kVK_ANSI_V, true) : NULL;
    CGEventRef up = source ? CGEventCreateKeyboardEvent(source, kVK_ANSI_V, false) : NULL;
    vt_insert_result result = vt_result(VT_INSERT_ALLOCATION, VT_INSERT_NOT_SUBMITTED, 0);
    if (down && up) {
        CGEventSetFlags(down, kCGEventFlagMaskCommand);
        CGEventSetFlags(up, kCGEventFlagMaskCommand);
        int writable = 0;
        result = vt_insert_state(target, guard, &writable);
        if (result.status == VT_INSERT_OK && !CGPreflightPostEventAccess())
            result = vt_result(VT_INSERT_PERMISSION, VT_INSERT_NOT_SUBMITTED, 0);
        if (result.status == VT_INSERT_OK) result = vt_check_focus(target, guard);
        if (result.status == VT_INSERT_OK && !vt_pasteboard_owned(transaction))
            result = vt_result(VT_INSERT_CLIPBOARD_CHANGED, VT_INSERT_NOT_SUBMITTED, 0);
        if (result.status == VT_INSERT_OK) result = vt_before_call(guard);
        if (result.status == VT_INSERT_OK) {
            CGEventPostToPid(target->pid, down);
            CGEventPostToPid(target->pid, up);
            result = vt_result(VT_INSERT_OK, VT_INSERT_SUBMITTED, 0);
        }
    }
    if (up) CFRelease(up);
    if (down) CFRelease(down);
    if (source) CFRelease(source);
    return result;
}
