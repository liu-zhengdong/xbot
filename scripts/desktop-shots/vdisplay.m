// CI only: a HiDPI virtual display next to the runner's 1x one; the window opens on it and is shot at 2x.
// Hosted macOS runners have one 1x display. CGVirtualDisplay is the CoreGraphics
// class Chromium's tests use for the same thing; it is not public API.
// Runs until killed; the display goes away with the process.
#import <Foundation/Foundation.h>
#import <CoreGraphics/CoreGraphics.h>

@interface CGVirtualDisplayDescriptor : NSObject
@property(retain, nonatomic) dispatch_queue_t queue;
@property(retain, nonatomic) NSString *name;
@property(nonatomic) unsigned int maxPixelsHigh;
@property(nonatomic) unsigned int maxPixelsWide;
@property(nonatomic) CGSize sizeInMillimeters;
@property(nonatomic) unsigned int serialNum;
@property(nonatomic) unsigned int productID;
@property(nonatomic) unsigned int vendorID;
@property(copy, nonatomic) void (^terminationHandler)(id, id);
@end
@interface CGVirtualDisplayMode : NSObject
- (instancetype)initWithWidth:(unsigned int)width height:(unsigned int)height refreshRate:(double)rate;
@end
@interface CGVirtualDisplaySettings : NSObject
@property(retain, nonatomic) NSArray *modes;
@property(nonatomic) unsigned int hiDPI;
@end
@interface CGVirtualDisplay : NSObject
- (instancetype)initWithDescriptor:(CGVirtualDisplayDescriptor *)descriptor;
- (BOOL)applySettings:(CGVirtualDisplaySettings *)settings;
@property(readonly, nonatomic) unsigned int displayID;
@end

int main(void) {
  @autoreleasepool {
    CGVirtualDisplayDescriptor *d = [[CGVirtualDisplayDescriptor alloc] init];
    d.queue = dispatch_get_main_queue();
    d.name = @"Shots 2x";
    d.maxPixelsWide = 2880; d.maxPixelsHigh = 1800;
    d.sizeInMillimeters = CGSizeMake(300, 188);
    d.serialNum = 1; d.productID = 0x1234; d.vendorID = 0x3456;
    CGVirtualDisplay *v = [[CGVirtualDisplay alloc] initWithDescriptor:d];
    if (!v) { fprintf(stderr, "vdisplay: no CGVirtualDisplay\n"); return 1; }
    CGVirtualDisplaySettings *s = [[CGVirtualDisplaySettings alloc] init];
    s.hiDPI = 1;
    s.modes = @[ [[CGVirtualDisplayMode alloc] initWithWidth:1440 height:900 refreshRate:60] ];
    if (![v applySettings:s]) { fprintf(stderr, "vdisplay: applySettings failed\n"); return 1; }
    CGDirectDisplayID vid = v.displayID;
    sleep(2);
    CGRect r = CGDisplayBounds(vid);
    CGDisplayModeRef m = CGDisplayCopyDisplayMode(vid);
    printf("vdisplay %u: %zux%zu pixels, %zux%zu points\n", vid,
           m ? CGDisplayModeGetPixelWidth(m) : 0, m ? CGDisplayModeGetPixelHeight(m) : 0,
           CGDisplayPixelsWide(vid), CGDisplayPixelsHigh(vid));
    // macos.sh puts the window on this display: it reads this line.
    printf("vdisplay-origin %d %d\n", (int)r.origin.x, (int)r.origin.y);
    fflush(stdout);
    [[NSRunLoop mainRunLoop] run];
  }
  return 0;
}
