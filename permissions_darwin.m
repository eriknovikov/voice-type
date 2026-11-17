#import <AVFoundation/AVFoundation.h>
#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>
#import "permissions_darwin.h"

int vt_microphone_status(void) {
    @autoreleasepool {
        switch ([AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeAudio]) {
            case AVAuthorizationStatusAuthorized: return 1;
            case AVAuthorizationStatusDenied: return 2;
            case AVAuthorizationStatusRestricted: return 3;
            default: return 0;
        }
    }
}

int vt_request_microphone(void) {
    @autoreleasepool {
        if ([[[NSBundle mainBundle] objectForInfoDictionaryKey:@"NSMicrophoneUsageDescription"] length] == 0) {
            return 0;
        }
    }
    dispatch_async(dispatch_get_main_queue(), ^{
        @autoreleasepool {
            // No Go pointers/callbacks are retained. The frontend reads status
            // again after the asynchronous system prompt finishes.
            if (vt_microphone_status() == 0) {
                [AVCaptureDevice requestAccessForMediaType:AVMediaTypeAudio completionHandler:^(BOOL granted) {
                    (void)granted;
                }];
            }
        }
    });
    return 1;
}

int vt_accessibility_trusted(void) { return AXIsProcessTrusted() ? 1 : 0; }

void vt_request_accessibility(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        @autoreleasepool {
            NSDictionary *options = @{(__bridge NSString *)kAXTrustedCheckOptionPrompt: @YES};
            (void)AXIsProcessTrustedWithOptions((__bridge CFDictionaryRef)options);
        }
    });
}

void vt_open_permission_settings(int microphone) {
    dispatch_async(dispatch_get_main_queue(), ^{
        @autoreleasepool {
            NSString *url = microphone
                ? @"x-apple.systempreferences:com.apple.preference.security?Privacy_Microphone"
                : @"x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility";
            [[NSWorkspace sharedWorkspace] openURL:[NSURL URLWithString:url]];
        }
    });
}
