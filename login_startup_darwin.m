#import <AppKit/AppKit.h>
#import <ServiceManagement/ServiceManagement.h>

int vt_login_startup_status(void) {
    @autoreleasepool {
        if (![[[NSBundle mainBundle] bundlePath] hasSuffix:@".app"]) return -1;
        @try {
            switch ([SMAppService mainAppService].status) {
                case SMAppServiceStatusEnabled: return 1;
                case SMAppServiceStatusRequiresApproval: return 2;
                case SMAppServiceStatusNotRegistered: return 0;
                default: return -1;
            }
        } @catch (NSException *exception) { (void)exception; return -1; }
    }
}
