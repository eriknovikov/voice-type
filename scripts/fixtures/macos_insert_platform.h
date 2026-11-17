#ifndef VOICE_TYPE_FAKE_MACOS_INSERT_PLATFORM_H
#define VOICE_TYPE_FAKE_MACOS_INSERT_PLATFORM_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <sys/types.h>
#include <unistd.h>

typedef struct fake_cf {
    int type;
    const char *name;
    int references;
    bool heap;
    bool key_down;
    int key;
    uint64_t flags;
    uint16_t text[1024];
    size_t length;
} fake_cf;

typedef fake_cf *CFTypeRef;
typedef fake_cf *CFStringRef;
typedef fake_cf *AXUIElementRef;
typedef fake_cf *CGEventSourceRef;
typedef fake_cf *CGEventRef;
typedef long CFIndex;
typedef int AXError;
typedef bool Boolean;

enum { kAXErrorSuccess, kAXErrorFailure, kAXErrorIllegalArgument,
       kAXErrorAttributeUnsupported, kAXErrorNoValue, kAXErrorCannotComplete };
enum { FAKE_ELEMENT = 1, FAKE_STRING, FAKE_BOOLEAN, FAKE_EVENT, FAKE_SOURCE };
enum { kCGEventSourceStatePrivate = -1, PROC_PIDTBSDINFO = 3 };
enum { kVK_ANSI_V = 9, kCGEventFlagMaskCommand = 1 << 20 };

#define FAKE_CONSTANT(symbol, type_value) static fake_cf fake_##symbol = {.type = type_value, .name = #symbol}
FAKE_CONSTANT(kAXFocusedApplicationAttribute, FAKE_STRING);
FAKE_CONSTANT(kAXFocusedUIElementAttribute, FAKE_STRING);
FAKE_CONSTANT(kAXRoleAttribute, FAKE_STRING);
FAKE_CONSTANT(kAXSubroleAttribute, FAKE_STRING);
FAKE_CONSTANT(kAXEnabledAttribute, FAKE_STRING);
FAKE_CONSTANT(kAXSelectedTextAttribute, FAKE_STRING);
FAKE_CONSTANT(kAXValueAttribute, FAKE_STRING);
FAKE_CONSTANT(kAXTextAreaRole, FAKE_STRING);
FAKE_CONSTANT(kAXTextFieldRole, FAKE_STRING);
FAKE_CONSTANT(kAXSecureTextFieldSubrole, FAKE_STRING);
FAKE_CONSTANT(kAXUnknownSubrole, FAKE_STRING);

#define kAXFocusedApplicationAttribute (&fake_kAXFocusedApplicationAttribute)
#define kAXFocusedUIElementAttribute (&fake_kAXFocusedUIElementAttribute)
#define kAXRoleAttribute (&fake_kAXRoleAttribute)
#define kAXSubroleAttribute (&fake_kAXSubroleAttribute)
#define kAXEnabledAttribute (&fake_kAXEnabledAttribute)
#define kAXSelectedTextAttribute (&fake_kAXSelectedTextAttribute)
#define kAXValueAttribute (&fake_kAXValueAttribute)
#define kAXTextAreaRole (&fake_kAXTextAreaRole)
#define kAXTextFieldRole (&fake_kAXTextFieldRole)
#define kAXSecureTextFieldSubrole (&fake_kAXSecureTextFieldSubrole)
#define kAXUnknownSubrole (&fake_kAXUnknownSubrole)

struct proc_bsdinfo { uint64_t pbi_start_tvsec, pbi_start_tvusec; };
int proc_pidinfo(pid_t pid, int flavor, uint64_t arg, void *buffer, int size);
Boolean AXIsProcessTrusted(void);
Boolean IsSecureEventInputEnabled(void);
AXUIElementRef AXUIElementCreateSystemWide(void);
AXError AXUIElementSetMessagingTimeout(AXUIElementRef element, float seconds);
AXError AXUIElementCopyAttributeValue(AXUIElementRef element, CFStringRef attribute, CFTypeRef *out);
AXError AXUIElementGetPid(AXUIElementRef element, pid_t *pid);
AXError AXUIElementIsAttributeSettable(AXUIElementRef element, CFStringRef attribute, Boolean *settable);
AXError AXUIElementSetAttributeValue(AXUIElementRef element, CFStringRef attribute, CFTypeRef value);
int AXUIElementGetTypeID(void);
int CFStringGetTypeID(void);
int CFBooleanGetTypeID(void);
int CFGetTypeID(CFTypeRef value);
Boolean CFBooleanGetValue(CFTypeRef value);
Boolean CFEqual(CFTypeRef a, CFTypeRef b);
void CFRelease(CFTypeRef value);
CFStringRef CFStringCreateWithCharacters(void *allocator, const uint16_t *text, CFIndex length);
CGEventSourceRef CGEventSourceCreate(int state);
CGEventRef CGEventCreateKeyboardEvent(CGEventSourceRef source, int key, bool down);
void CGEventSetFlags(CGEventRef event, uint64_t flags);
void CGEventKeyboardSetUnicodeString(CGEventRef event, size_t length, const uint16_t *text);
Boolean CGPreflightPostEventAccess(void);
void CGEventPostToPid(pid_t pid, CGEventRef event);

#endif
