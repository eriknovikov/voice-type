#import <AppKit/AppKit.h>
#import "paste_darwin.h"

static NSString *const VTPasteTokenType = @"io.github.eriknovikov.voice-type.paste-token";
static const NSUInteger VTPasteSnapshotLimit = 32 * 1024 * 1024;

@interface VTPasteTransaction : NSObject <NSPasteboardItemDataProvider> {
@public
    NSPasteboard *board;
    NSArray *snapshot;
    NSData *payload;
    NSData *token;
    NSInteger ownedCount;
    uint64_t readRequests;
    BOOL published;
    BOOL tokenWritten;
}
- (BOOL)ownsPasteboard;
@end

@implementation VTPasteTransaction
- (BOOL)ownsPasteboard {
    if (!published || board.changeCount != ownedCount) return NO;
    NSData *current = [board dataForType:VTPasteTokenType];
    if (current && [current isEqualToData:token]) return board.changeCount == ownedCount;
    return !tokenWritten && board.pasteboardItems.count == 0 && board.changeCount == ownedCount;
}

- (void)pasteboard:(NSPasteboard *)pasteboard item:(NSPasteboardItem *)item provideDataForType:(NSPasteboardType)type {
    (void)pasteboard;
    @synchronized (self) {
        if ([type isEqualToString:NSPasteboardTypeString]) {
            readRequests++;
            [item setData:payload forType:type];
        }
    }
}

- (void)dealloc {
    [board release];
    [snapshot release];
    [payload release];
    [token release];
    [super dealloc];
}
@end

static BOOL vt_paste_on_main(dispatch_block_t action) {
    __block BOOL succeeded = YES;
    dispatch_block_t guarded = ^{
        @try { action(); }
        @catch (NSException *exception) { (void)exception; succeeded = NO; }
    };
    if ([NSThread isMainThread]) guarded();
    else dispatch_sync(dispatch_get_main_queue(), guarded);
    return succeeded;
}

int vt_pasteboard_begin(const char *text, size_t length, void **out) {
    *out = NULL;
    __block int status = VT_PASTE_OK;
    BOOL succeeded = vt_paste_on_main(^{
        @autoreleasepool {
            VTPasteTransaction *transaction = [[VTPasteTransaction alloc] init];
            if (!transaction) { status = VT_PASTE_SNAPSHOT_ERROR; return; }
            *out = transaction;
            transaction->board = [[NSPasteboard generalPasteboard] retain];
            transaction->payload = [[NSData alloc] initWithBytes:text length:length];
            transaction->token = [[[[NSUUID UUID] UUIDString] dataUsingEncoding:NSUTF8StringEncoding] retain];
            NSInteger before = transaction->board.changeCount;
            NSArray<NSPasteboardItem *> *items = transaction->board.pasteboardItems;
            NSMutableArray *saved = [NSMutableArray array];
            NSUInteger total = 0;
            if (!items || !transaction->payload || !transaction->token) status = VT_PASTE_SNAPSHOT_ERROR;
            if (items.count > 128) status = VT_PASTE_TOO_LARGE;
            for (NSPasteboardItem *item in items) {
                if (status != VT_PASTE_OK) break;
                NSPasteboardItem *copy = [[[NSPasteboardItem alloc] init] autorelease];
                if (item.types.count == 0 || item.types.count > 128) { status = VT_PASTE_SNAPSHOT_ERROR; break; }
                for (NSPasteboardType type in item.types) {
                    NSData *data = [item dataForType:type];
                    if (!data) { status = VT_PASTE_SNAPSHOT_ERROR; break; }
                    if (data.length > VTPasteSnapshotLimit - total) { status = VT_PASTE_TOO_LARGE; break; }
                    total += data.length;
                    if (![copy setData:data forType:type]) { status = VT_PASTE_SNAPSHOT_ERROR; break; }
                }
                [saved addObject:copy];
            }
            transaction->snapshot = [saved copy];
            if (transaction->board.changeCount != before) status = VT_PASTE_CHANGED;
            if (status != VT_PASTE_OK) return;

            NSPasteboardItem *replacement = [[[NSPasteboardItem alloc] init] autorelease];
            BOOL prepared = [replacement setDataProvider:transaction forTypes:@[NSPasteboardTypeString]];
            prepared = prepared && [replacement setData:[NSData data] forType:@"org.nspasteboard.TransientType"];
            prepared = prepared && [replacement setData:[NSData data] forType:@"org.nspasteboard.ConcealedType"];
            prepared = prepared && [replacement setData:transaction->token forType:VTPasteTokenType];
            if (!prepared || transaction->board.changeCount != before) {
                status = prepared ? VT_PASTE_CHANGED : VT_PASTE_WRITE_ERROR;
                return;
            }
            transaction->ownedCount = [transaction->board clearContents];
            transaction->published = YES;
            if (transaction->board.changeCount != transaction->ownedCount) { status = VT_PASTE_CHANGED; return; }
            if (![transaction->board writeObjects:@[replacement]]) { status = VT_PASTE_WRITE_ERROR; return; }
            transaction->tokenWritten = YES;
            transaction->ownedCount = transaction->board.changeCount;
            if (![transaction ownsPasteboard]) status = VT_PASTE_CHANGED;
        }
    });
    return succeeded ? status : VT_PASTE_NATIVE_ERROR;
}

int vt_pasteboard_state(void *value, int *owned, uint64_t *reads) {
    __block int status = VT_PASTE_OK;
    BOOL succeeded = vt_paste_on_main(^{
        @autoreleasepool {
            VTPasteTransaction *transaction = (VTPasteTransaction *)value;
            if (!transaction) { status = VT_PASTE_SNAPSHOT_ERROR; return; }
            *owned = [transaction ownsPasteboard] ? 1 : 0;
            @synchronized (transaction) { *reads = transaction->readRequests; }
        }
    });
    return succeeded ? status : VT_PASTE_NATIVE_ERROR;
}

int vt_pasteboard_owned(void *transaction) {
    int owned = 0;
    uint64_t reads = 0;
    return vt_pasteboard_state(transaction, &owned, &reads) == VT_PASTE_OK && owned;
}

int vt_pasteboard_restore(void *value) {
    __block int status = VT_PASTE_OK;
    BOOL succeeded = vt_paste_on_main(^{
        @autoreleasepool {
            VTPasteTransaction *transaction = (VTPasteTransaction *)value;
            if (!transaction) return;
            if (![transaction ownsPasteboard]) {
                transaction->published = NO;
                return;
            }
            NSInteger cleared = [transaction->board clearContents];
            transaction->published = NO;
            if (transaction->board.changeCount != cleared) return;
            if (transaction->snapshot.count > 0 && ![transaction->board writeObjects:transaction->snapshot])
                status = VT_PASTE_WRITE_ERROR;
        }
    });
    return succeeded ? status : VT_PASTE_NATIVE_ERROR;
}

void vt_pasteboard_close(void *value) {
    vt_paste_on_main(^{
        @autoreleasepool { [(VTPasteTransaction *)value release]; }
    });
}
