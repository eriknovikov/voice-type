#define VT_INSERT_TEST
#include "../../insert_darwin.c"

#include <assert.h>
#include <stdio.h>
#include <string.h>

static fake_cf system_element = {.type = FAKE_ELEMENT, .name = "system"};
static fake_cf application = {.type = FAKE_ELEMENT, .name = "application"};
static fake_cf field = {.type = FAKE_ELEMENT, .name = "field"};
static fake_cf other_field = {.type = FAKE_ELEMENT, .name = "other-field"};
static fake_cf ordinary_subrole = {.type = FAKE_STRING, .name = "ordinary"};
static fake_cf enabled_value = {.type = FAKE_BOOLEAN, .name = "true"};

static struct {
    bool trusted, secure_input, post_access, selected_writable, value_writable;
    bool enabled, other_focus, process_alive, wrong_application;
    bool allocation_failure, cancel_on_down, cancel_on_preflight, focus_on_preflight;
    bool unsafe_during_query, cancel_during_query, reused_pid;
    bool clipboard_owned;
    AXError selected_error, value_error, setter_error, subrole_error, focus_error;
    CFTypeRef role, subrole;
    pid_t pid;
    int setters, key_downs, key_ups, allocations, references, queries;
    vt_insert_guard *guard;
    uint16_t submitted[1024];
    size_t submitted_length;
} fixture;

static void reset(void) {
    assert(fixture.references == 0 && fixture.allocations == 0);
    memset(&fixture, 0, sizeof(fixture));
    fixture.trusted = fixture.post_access = fixture.enabled = fixture.process_alive = true;
    fixture.selected_writable = fixture.value_writable = true;
    fixture.clipboard_owned = true;
    fixture.role = kAXTextAreaRole;
    fixture.subrole = &ordinary_subrole;
    fixture.pid = getpid() + 100;
}

static fake_cf *retain(fake_cf *value) {
    value->references++;
    fixture.references++;
    return value;
}

static fake_cf *allocate(int type) {
    if (fixture.allocation_failure) return NULL;
    fake_cf *value = calloc(1, sizeof(*value));
    assert(value);
    value->type = type;
    value->heap = true;
    fixture.allocations++;
    return retain(value);
}

int proc_pidinfo(pid_t pid, int flavor, uint64_t arg, void *buffer, int size) {
    assert(pid == fixture.pid && flavor == PROC_PIDTBSDINFO && arg == 0);
    if (!fixture.process_alive) return 0;
    struct proc_bsdinfo info = {123, fixture.reused_pid ? 457 : 456};
    memcpy(buffer, &info, sizeof(info));
    return size;
}

Boolean AXIsProcessTrusted(void) { return fixture.trusted; }
Boolean IsSecureEventInputEnabled(void) { return fixture.secure_input; }
AXUIElementRef AXUIElementCreateSystemWide(void) { return retain(&system_element); }
AXError AXUIElementSetMessagingTimeout(AXUIElementRef element, float seconds) {
    assert(element && seconds == 0.25f);
    return kAXErrorSuccess;
}

AXError AXUIElementCopyAttributeValue(AXUIElementRef element, CFStringRef attribute, CFTypeRef *out) {
    assert(element);
    fixture.queries++;
    *out = NULL;
    if (attribute == kAXFocusedApplicationAttribute) { *out = retain(&application); return kAXErrorSuccess; }
    if (attribute == kAXFocusedUIElementAttribute) {
        if (fixture.focus_error) return fixture.focus_error;
        *out = retain(fixture.other_focus ? &other_field : &field);
        return kAXErrorSuccess;
    }
    assert(element == &field);
    if (fixture.cancel_during_query) vt_insert_guard_cancel(fixture.guard);
    if (fixture.unsafe_during_query) fixture.secure_input = true;
    if (attribute == kAXRoleAttribute) *out = retain(fixture.role);
    else if (attribute == kAXSubroleAttribute) {
        if (fixture.subrole_error) return fixture.subrole_error;
        *out = retain(fixture.subrole);
    } else if (attribute == kAXEnabledAttribute) *out = retain(&enabled_value);
    else assert(!"must not read selected text, AXValue or surrounding content");
    return kAXErrorSuccess;
}

AXError AXUIElementGetPid(AXUIElementRef element, pid_t *pid) {
    *pid = fixture.pid + (element == &application && fixture.wrong_application ? 1 : 0);
    return kAXErrorSuccess;
}

AXError AXUIElementIsAttributeSettable(AXUIElementRef element, CFStringRef attribute, Boolean *settable) {
    assert(element == &field);
    if (attribute == kAXSelectedTextAttribute) { *settable = fixture.selected_writable; return fixture.selected_error; }
    assert(attribute == kAXValueAttribute);
    *settable = fixture.value_writable;
    return fixture.value_error;
}

AXError AXUIElementSetAttributeValue(AXUIElementRef element, CFStringRef attribute, CFTypeRef value) {
    assert(element == &field && attribute == kAXSelectedTextAttribute && value->type == FAKE_STRING);
    fixture.setters++;
    memcpy(fixture.submitted, value->text, value->length * sizeof(uint16_t));
    fixture.submitted_length = value->length;
    return fixture.setter_error;
}

int AXUIElementGetTypeID(void) { return FAKE_ELEMENT; }
int CFStringGetTypeID(void) { return FAKE_STRING; }
int CFBooleanGetTypeID(void) { return FAKE_BOOLEAN; }
int CFGetTypeID(CFTypeRef value) { return value->type; }
Boolean CFBooleanGetValue(CFTypeRef value) { assert(value == &enabled_value); return fixture.enabled; }
Boolean CFEqual(CFTypeRef a, CFTypeRef b) { return a == b; }
void CFRelease(CFTypeRef value) {
    assert(value && value->references > 0);
    value->references--;
    fixture.references--;
    if (value->heap && value->references == 0) { fixture.allocations--; free(value); }
}

CFStringRef CFStringCreateWithCharacters(void *allocator, const uint16_t *text, CFIndex length) {
    assert(!allocator && length > 0 && length <= 1024);
    fake_cf *value = allocate(FAKE_STRING);
    if (value) { memcpy(value->text, text, length * sizeof(uint16_t)); value->length = length; }
    return value;
}

CGEventSourceRef CGEventSourceCreate(int state) { assert(state == kCGEventSourceStatePrivate); return allocate(FAKE_SOURCE); }
CGEventRef CGEventCreateKeyboardEvent(CGEventSourceRef source, int key, bool down) {
    assert(source && source->type == FAKE_SOURCE && (key == 0 || key == kVK_ANSI_V));
    fake_cf *event = allocate(FAKE_EVENT);
    if (event) { event->key_down = down; event->key = key; }
    return event;
}
void CGEventSetFlags(CGEventRef event, uint64_t flags) {
    assert(event && flags == (event->key == kVK_ANSI_V ? kCGEventFlagMaskCommand : 0));
    event->flags = flags;
}
void CGEventKeyboardSetUnicodeString(CGEventRef event, size_t length, const uint16_t *text) {
    assert(event && length > 0 && length <= 20);
    memcpy(event->text, text, length * sizeof(uint16_t));
    event->length = length;
}
Boolean CGPreflightPostEventAccess(void) {
    if (fixture.cancel_on_preflight) vt_insert_guard_cancel(fixture.guard);
    if (fixture.focus_on_preflight) fixture.other_focus = true;
    return fixture.post_access;
}
void CGEventPostToPid(pid_t pid, CGEventRef event) {
    assert(pid == fixture.pid && event && event->type == FAKE_EVENT);
    if (event->key_down) {
        fixture.key_downs++;
        memcpy(fixture.submitted, event->text, event->length * sizeof(uint16_t));
        fixture.submitted_length = event->length;
        if (fixture.cancel_on_down) vt_insert_guard_cancel(fixture.guard);
    } else fixture.key_ups++;
}

int vt_pasteboard_owned(void *transaction) { assert(transaction); return fixture.clipboard_owned; }

static vt_insert_target *capture(void) {
    fixture.guard = vt_insert_guard_create();
    assert(fixture.guard);
    vt_insert_target *target = NULL;
    vt_insert_result result = vt_insert_capture(fixture.guard, &target);
    assert(result.status == VT_INSERT_OK && target);
    return target;
}

static void cleanup(vt_insert_target *target) {
    vt_insert_close(target);
    vt_insert_guard_free(fixture.guard);
    fixture.guard = NULL;
    assert(fixture.references == 0 && fixture.allocations == 0);
}

static const uint16_t unicode_text[] = {'a', '\n', 0xd83d, 0xde42};

static void test_selected_text(void) {
    reset();
    vt_insert_target *target = capture();
    vt_insert_result result = vt_insert_replace(target, fixture.guard, unicode_text, 4);
    assert(result.status == VT_INSERT_OK && result.submission == VT_INSERT_SUBMITTED);
    assert(fixture.setters == 1 && fixture.key_downs == 0 && fixture.submitted_length == 4);
    assert(memcmp(fixture.submitted, unicode_text, sizeof(unicode_text)) == 0);
    fixture.setter_error = kAXErrorAttributeUnsupported;
    result = vt_insert_replace(target, fixture.guard, unicode_text, 4);
    assert(result.status == VT_INSERT_NATIVE_ERROR && result.submission == VT_INSERT_UNKNOWN);
    assert(fixture.setters == 2 && fixture.key_downs == 0);
    cleanup(target);
    puts("PASS selected-text only, Unicode payload, setter errors never authorize fallback");
}

static void test_unicode(void) {
    reset();
    vt_insert_target *target = capture();
    fixture.selected_error = kAXErrorAttributeUnsupported;
    vt_insert_result result = vt_insert_replace(target, fixture.guard, unicode_text, 4);
    assert(result.status == VT_INSERT_UNSUPPORTED && result.submission == VT_INSERT_NOT_SUBMITTED && fixture.setters == 0);
    result = vt_insert_unicode(target, fixture.guard, unicode_text, 4);
    assert(result.status == VT_INSERT_OK && result.submission == VT_INSERT_SUBMITTED);
    assert(fixture.key_downs == 1 && fixture.key_ups == 1 && fixture.submitted_length == 4);
    assert(memcmp(fixture.submitted, unicode_text, sizeof(unicode_text)) == 0);
    fixture.cancel_on_down = true;
    result = vt_insert_unicode(target, fixture.guard, unicode_text, 4);
    assert(result.status == VT_INSERT_OK && fixture.key_downs == 2 && fixture.key_ups == 2);
    cleanup(target);
    puts("PASS Unicode process-targeted balanced events, no modifier inheritance, cancellation after down");
}

static void test_refusal(void) {
    for (int scenario = 0; scenario < 19; scenario++) {
        reset();
        vt_insert_target *target = capture();
        switch (scenario) {
            case 0: fixture.trusted = false; break;
            case 1: fixture.secure_input = true; break;
            case 2: fixture.other_focus = true; break;
            case 3: fixture.wrong_application = true; break;
            case 4: fixture.process_alive = false; break;
            case 5: fixture.reused_pid = true; break;
            case 6: fixture.subrole = kAXSecureTextFieldSubrole; break;
            case 7: fixture.role = &ordinary_subrole; break;
            case 8: fixture.enabled = false; break;
            case 9: fixture.selected_writable = fixture.value_writable = false; break;
            case 10: fixture.selected_error = kAXErrorCannotComplete; break;
            case 11: fixture.cancel_during_query = true; break;
            case 12: fixture.unsafe_during_query = true; break;
            case 13: fixture.subrole = kAXUnknownSubrole; break;
            case 14: fixture.role = kAXTextFieldRole; fixture.subrole_error = kAXErrorNoValue; break;
            case 15: fixture.subrole = &enabled_value; break;
            case 16: fixture.subrole_error = kAXErrorCannotComplete; break;
            case 17: fixture.focus_error = kAXErrorCannotComplete; break;
            case 18: vt_insert_guard_cancel(fixture.guard); break;
        }
        vt_insert_result result = vt_insert_replace(target, fixture.guard, unicode_text, 4);
        assert(result.status != VT_INSERT_OK && result.submission == VT_INSERT_NOT_SUBMITTED);
        assert(fixture.setters == 0 && fixture.key_downs == 0 && fixture.key_ups == 0);
        cleanup(target);
    }
    puts("PASS 19 native refusal paths including PID reuse, focus, protected/unknown/read-only targets and late cancellation");
}

static void test_unicode_refusal(void) {
    for (int scenario = 0; scenario < 6; scenario++) {
        reset();
        vt_insert_target *target = capture();
        fixture.selected_writable = false;
        switch (scenario) {
            case 0: fixture.post_access = false; break;
            case 1: fixture.cancel_on_preflight = true; break;
            case 2: fixture.focus_on_preflight = true; break;
            case 3: fixture.selected_writable = true; break;
            case 4: fixture.allocation_failure = true; break;
            case 5: fixture.value_writable = false; break;
        }
        vt_insert_result result = vt_insert_unicode(target, fixture.guard, unicode_text, 4);
        assert(result.status != VT_INSERT_OK && result.submission == VT_INSERT_NOT_SUBMITTED);
        assert(fixture.setters == 0 && fixture.key_downs == 0 && fixture.key_ups == 0);
        cleanup(target);
    }
    puts("PASS Unicode access/focus/cancellation/capability/allocation refusal without posting");
}

static void test_capture(void) {
    reset();
    fixture.guard = vt_insert_guard_create();
    fixture.role = kAXTextFieldRole;
    fixture.subrole = kAXSecureTextFieldSubrole;
    vt_insert_target *target = NULL;
    vt_insert_result result = vt_insert_capture(fixture.guard, &target);
    assert(result.status == VT_INSERT_UNSAFE && target == NULL);
    cleanup(target);
    reset();
    fixture.guard = vt_insert_guard_create();
    fixture.pid = getpid();
    result = vt_insert_capture(fixture.guard, &target);
    assert(result.status == VT_INSERT_FOCUS_CHANGED && target == NULL);
    cleanup(target);
    reset();
    fixture.subrole_error = kAXErrorNoValue;
    target = capture();
    cleanup(target);
    puts("PASS capture cleanup, own-app refusal, optional text-area subrole");
}

static void test_command_paste(void) {
    for (int scenario = 0; scenario < 5; scenario++) {
        reset();
        vt_insert_target *target = capture();
        switch (scenario) {
            case 0: break;
            case 1: fixture.clipboard_owned = false; break;
            case 2: fixture.focus_on_preflight = true; break;
            case 3: fixture.cancel_on_preflight = true; break;
            case 4: fixture.post_access = false; break;
        }
        vt_insert_result result = vt_insert_paste(target, fixture.guard, &fixture);
        if (scenario == 0) {
            assert(result.status == VT_INSERT_OK && result.submission == VT_INSERT_SUBMITTED);
            assert(fixture.key_downs == 1 && fixture.key_ups == 1 && fixture.submitted_length == 0);
        } else {
            assert(result.status != VT_INSERT_OK && result.submission == VT_INSERT_NOT_SUBMITTED);
            assert(fixture.key_downs == 0 && fixture.key_ups == 0);
        }
        assert(fixture.setters == 0);
        cleanup(target);
    }
    puts("PASS one Command-V with pinned process and final clipboard ownership/access/focus checks");
}

int main(void) {
    test_selected_text();
    test_unicode();
    test_refusal();
    test_unicode_refusal();
    test_capture();
    test_command_paste();
    puts("PASS native control-flow fixture; not Apple SDK or macOS runtime verification");
    return 0;
}
