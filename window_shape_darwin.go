//go:build darwin && !ios && !server

package main

/*
#cgo CFLAGS: -mmacosx-version-min=10.13 -x objective-c
#cgo LDFLAGS: -framework Cocoa

#import <Cocoa/Cocoa.h>
#import <objc/message.h>

// -[NSWindow _cornerRadius] is private, read-only and has existed since the
// rounded-window era; it is what AppKit itself draws the frame with. Resolved
// at runtime, its return type checked before the call, and exception guarded:
// if Apple removes or changes it this returns -1 and the caller falls back to
// the documented value for the OS version. Nothing is written.
static double devkitWindowCornerRadius(void *handle) {
	@try {
		NSWindow *window = (NSWindow *)handle;
		SEL selector = NSSelectorFromString(@"_cornerRadius");
		if (window == nil || ![window respondsToSelector:selector]) {
			return -1;
		}
		NSMethodSignature *signature = [window methodSignatureForSelector:selector];
		if (signature == nil || signature.numberOfArguments != 2 || signature.methodReturnType == NULL) {
			return -1;
		}
		char type = signature.methodReturnType[0];
		if (type == 'd') {
			double (*send)(id, SEL) = (double (*)(id, SEL))objc_msgSend;
			return send(window, selector);
		}
		if (type == 'f') {
			float (*send)(id, SEL) = (float (*)(id, SEL))objc_msgSend;
			return send(window, selector);
		}
		return -1;
	} @catch (NSException *exception) {
		return -1;
	}
}

static int devkitMacOSMajorVersion(void) {
	return (int)[[NSProcessInfo processInfo] operatingSystemVersion].majorVersion;
}
*/
import "C"

import "github.com/wailsapp/wails/v3/pkg/application"

// macOS window radius, from AppKit when it will say. Measured on macOS 27 it
// reports 16 for every title bar and toolbar style, including DevKit's
// unified-compact one.
func nativeWindowCornerRadius(win application.Window) (float64, bool) {
	var radius float64
	var major int
	if win != nil && win.NativeWindow() != nil {
		handle := win.NativeWindow()
		application.InvokeSync(func() {
			radius = float64(C.devkitWindowCornerRadius(handle))
			major = int(C.devkitMacOSMajorVersion())
		})
	}
	if radius > 0 && radius < 64 {
		return radius, true
	}
	return macFallbackCornerRadius(major), true
}
