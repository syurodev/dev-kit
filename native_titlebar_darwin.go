//go:build darwin && !ios && !server

package main

/*
#cgo CFLAGS: -mmacosx-version-min=10.13 -x objective-c
#cgo LDFLAGS: -framework Cocoa

#import <Cocoa/Cocoa.h>

// A titlebar accessory view that never intercepts mouse events: hitTest
// always returns nil, so a click or drag in this region falls through to
// the WKWebView underneath (NSWindowStyleMaskFullSizeContentView means the
// webview already renders the entire window, titlebar area included — this
// accessory view exists purely to influence AppKit's own layout, not to
// display or handle anything itself).
@interface DevKitClickThroughSpacerView : NSView
@end

@implementation DevKitClickThroughSpacerView
- (NSView *)hitTest:(NSPoint)point {
	return nil;
}
@end

// devkitSetTitlebarHeight grows the native titlebar region to `height`
// points. AppKit re-centers the standard traffic-light buttons within
// whatever height the titlebar actually is, so this is what makes them
// line up with the frontend's own (taller than default) titlebar row
// instead of sitting near the top of it. Safe to call more than once:
// removes any spacer this function added previously first, so it can't
// stack accessories and keep growing the titlebar on repeated calls.
static void devkitSetTitlebarHeight(void *handle, double height) {
	NSWindow *window = (NSWindow *)handle;
	if (window == nil) {
		return;
	}

	NSMutableArray *staleIndexes = [NSMutableArray array];
	NSUInteger index = 0;
	for (NSTitlebarAccessoryViewController *accessory in window.titlebarAccessoryViewControllers) {
		if ([accessory.view isKindOfClass:[DevKitClickThroughSpacerView class]]) {
			[staleIndexes addObject:@(index)];
		}
		index++;
	}
	for (NSNumber *staleIndex in [staleIndexes reverseObjectEnumerator]) {
		[window removeTitlebarAccessoryViewControllerAtIndex:staleIndex.unsignedIntegerValue];
	}

	DevKitClickThroughSpacerView *spacer =
		[[DevKitClickThroughSpacerView alloc] initWithFrame:NSMakeRect(0, 0, 0, height)];
	[spacer setTranslatesAutoresizingMaskIntoConstraints:NO];
	[[spacer.heightAnchor constraintEqualToConstant:height] setActive:YES];

	NSTitlebarAccessoryViewController *accessory = [[NSTitlebarAccessoryViewController alloc] init];
	accessory.view = spacer;
	accessory.layoutAttribute = NSLayoutAttributeBottom;
	// The controller and view are retained by window.titlebarAccessoryViewControllers
	// / accessory.view respectively once attached — release our extra alloc
	// refs immediately rather than relying on an autorelease pool being
	// active on whatever thread this runs on.
	[window addTitlebarAccessoryViewController:accessory];
	[spacer release];
	[accessory release];
}
*/
import "C"

import "github.com/wailsapp/wails/v3/pkg/application"

// Wails' Mac.InvisibleTitleBarHeight (see main.go) is a mouse-down drag
// hit-zone only — it has no effect on how tall AppKit's actual titlebar is,
// so it can't be used to influence traffic-light position. This constant is
// the real lever: it must match the frontend header's own height
// (Workbench.tsx's `h-[45px]`) or the traffic lights and the tab row will
// drift apart again.
const devkitTitlebarHeightPx = 45

// setNativeTitlebarHeight grows the native titlebar to devkitTitlebarHeightPx
// so AppKit re-centers the traffic-light buttons within it. Best-effort: a
// missing native window is silently ignored, same as the other native_*_darwin
// helpers in this file set.
func setNativeTitlebarHeight(window application.Window) {
	if window == nil || window.NativeWindow() == nil {
		return
	}
	handle := window.NativeWindow()
	application.InvokeSync(func() {
		C.devkitSetTitlebarHeight(handle, C.double(devkitTitlebarHeightPx))
	})
}
