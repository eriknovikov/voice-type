#import <Foundation/Foundation.h>
#include <stdlib.h>
#include <string.h>

char *vt_doctor_environment(void) {
    @autoreleasepool {
        @try {
            NSBundle *bundle = [NSBundle mainBundle];
            NSDictionary *values = @{
                @"System": [NSProcessInfo processInfo].operatingSystemVersionString,
                @"MemoryBytes": @([NSProcessInfo processInfo].physicalMemory),
                @"BundlePath": bundle.bundlePath ?: @"",
                @"BundleID": bundle.bundleIdentifier ?: @"",
                @"BundleVersion": [bundle objectForInfoDictionaryKey:@"CFBundleShortVersionString"] ?: @"",
                @"MinimumOS": [bundle objectForInfoDictionaryKey:@"LSMinimumSystemVersion"] ?: @""
            };
            NSData *data = [NSJSONSerialization dataWithJSONObject:values options:0 error:NULL];
            if (!data) return NULL;
            char *result = malloc(data.length + 1);
            if (result) { memcpy(result, data.bytes, data.length); result[data.length] = '\0'; }
            return result;
        } @catch (NSException *exception) { (void)exception; return NULL; }
    }
}
