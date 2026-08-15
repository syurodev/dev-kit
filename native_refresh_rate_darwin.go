//go:build darwin && !ios && !server

package main

/*
#cgo CFLAGS: -mmacosx-version-min=10.13 -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework WebKit

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#import <objc/message.h>
#include <string.h>

static BOOL devkitSelectorReturnsObject(id target, SEL selector, NSUInteger argumentCount) {
	NSMethodSignature *signature = [target methodSignatureForSelector:selector];
	return signature != nil &&
		signature.numberOfArguments == argumentCount &&
		signature.methodReturnType != NULL &&
		signature.methodReturnType[0] == '@';
}

static BOOL devkitSetFeatureSignatureIsCompatible(id target, SEL selector) {
	NSMethodSignature *signature = [target methodSignatureForSelector:selector];
	if (signature == nil || signature.numberOfArguments != 4) {
		return NO;
	}
	const char *enabledType = [signature getArgumentTypeAtIndex:2];
	const char *featureType = [signature getArgumentTypeAtIndex:3];
	return enabledType != NULL && strcmp(enabledType, @encode(BOOL)) == 0 &&
		featureType != NULL && featureType[0] == '@';
}

// This is intentionally best-effort. Every private selector is resolved at
// runtime and the whole operation is exception guarded. If WebKit removes or
// changes any part of this API, returning 0 leaves WKWebView's default frame
// rate policy untouched.
static int devkitEnableNativeWebViewRefreshRate(void *handle) {
	@try {
		NSWindow *window = (NSWindow *)handle;
		if (window == nil) {
			return 0;
		}

		SEL webViewSelector = NSSelectorFromString(@"webView");
		if (![window respondsToSelector:webViewSelector] ||
			!devkitSelectorReturnsObject(window, webViewSelector, 2)) {
			return 0;
		}
		id (*sendObject)(id, SEL) = (id (*)(id, SEL))objc_msgSend;
		id candidate = sendObject(window, webViewSelector);
		if (![candidate isKindOfClass:[WKWebView class]]) {
			return 0;
		}

		WKWebView *webView = (WKWebView *)candidate;
		WKPreferences *preferences = webView.configuration.preferences;
		Class preferencesClass = NSClassFromString(@"WKPreferences");
		SEL featuresSelector = NSSelectorFromString(@"_features");
		SEL setEnabledSelector = NSSelectorFromString(@"_setEnabled:forFeature:");
		if (preferences == nil || preferencesClass == Nil ||
			![preferencesClass respondsToSelector:featuresSelector] ||
			![preferences respondsToSelector:setEnabledSelector] ||
			!devkitSelectorReturnsObject(preferencesClass, featuresSelector, 2) ||
			!devkitSetFeatureSignatureIsCompatible(preferences, setEnabledSelector)) {
			return 0;
		}

		id featuresValue = sendObject(preferencesClass, featuresSelector);
		if (![featuresValue isKindOfClass:[NSArray class]]) {
			return 0;
		}

		NSString *targetKey = @"PreferPageRenderingUpdatesNear60FPSEnabled";
		SEL keySelector = NSSelectorFromString(@"key");
		for (id feature in (NSArray *)featuresValue) {
			if (feature == nil || ![feature respondsToSelector:keySelector] ||
				!devkitSelectorReturnsObject(feature, keySelector, 2)) {
				continue;
			}
			id key = sendObject(feature, keySelector);
			if (![key isKindOfClass:[NSString class]] || ![(NSString *)key isEqualToString:targetKey]) {
				continue;
			}

			void (*setEnabled)(id, SEL, BOOL, id) =
				(void (*)(id, SEL, BOOL, id))objc_msgSend;
			setEnabled(preferences, setEnabledSelector, NO, feature);
			return 1;
		}
	} @catch (NSException *exception) {
		// A private API failure must never prevent the normal WKWebView from
		// rendering. Deliberately swallow it and use the default 60Hz policy.
		return 0;
	}

	return 0;
}
*/
import "C"

import "github.com/wailsapp/wails/v3/pkg/application"

func enableNativeHighRefreshRate(window application.Window) bool {
	if window == nil || window.NativeWindow() == nil {
		return false
	}

	enabled := false
	handle := window.NativeWindow()
	application.InvokeSync(func() {
		enabled = C.devkitEnableNativeWebViewRefreshRate(handle) == 1
	})
	return enabled
}
