//go:build darwin && cgo

// Package macos wraps the few native macOS APIs edrive needs: Touch ID,
// Keychain items owned by the edrive binary, screen-lock state and a
// clipboard that clipboard managers are asked not to record.
package macos

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation -framework AppKit -framework LocalAuthentication -framework Security -framework CoreGraphics
#import <Foundation/Foundation.h>
#import <AppKit/AppKit.h>
#import <LocalAuthentication/LocalAuthentication.h>
#import <Security/Security.h>
#import <CoreGraphics/CoreGraphics.h>
#include <stdlib.h>
#include <string.h>

static char *edrive_copy_error(NSError *err) {
	if (err == nil) return NULL;
	return strdup([[err localizedDescription] UTF8String]);
}

// 0 = authenticated, 1 = failed, 2 = unavailable, 3 = cancelled
static int edrive_authenticate(const char *reason, char **errOut) {
	@autoreleasepool {
		LAContext *ctx = [[LAContext alloc] init];
		NSError *err = nil;
		if (![ctx canEvaluatePolicy:LAPolicyDeviceOwnerAuthentication error:&err]) {
			*errOut = edrive_copy_error(err);
			return 2;
		}
		__block int result = 1;
		__block NSError *replyErr = nil;
		dispatch_semaphore_t done = dispatch_semaphore_create(0);
		[ctx evaluatePolicy:LAPolicyDeviceOwnerAuthentication
		    localizedReason:[NSString stringWithUTF8String:reason]
		              reply:^(BOOL success, NSError *e) {
			if (success) {
				result = 0;
			} else {
				replyErr = e;
				if (e.code == LAErrorUserCancel || e.code == LAErrorAppCancel || e.code == LAErrorSystemCancel) {
					result = 3;
				}
			}
			dispatch_semaphore_signal(done);
		}];
		dispatch_semaphore_wait(done, DISPATCH_TIME_FOREVER);
		*errOut = edrive_copy_error(replyErr);
		return result;
	}
}

static int edrive_can_authenticate(void) {
	@autoreleasepool {
		LAContext *ctx = [[LAContext alloc] init];
		return [ctx canEvaluatePolicy:LAPolicyDeviceOwnerAuthentication error:nil] ? 1 : 0;
	}
}

static NSMutableDictionary *edrive_query(const char *service, const char *account) {
	NSMutableDictionary *q = [NSMutableDictionary dictionary];
	q[(__bridge id)kSecClass] = (__bridge id)kSecClassGenericPassword;
	q[(__bridge id)kSecAttrService] = [NSString stringWithUTF8String:service];
	q[(__bridge id)kSecAttrAccount] = [NSString stringWithUTF8String:account];
	return q;
}

static OSStatus edrive_kc_get(const char *service, const char *account, void **out, int *outLen) {
	@autoreleasepool {
		NSMutableDictionary *q = edrive_query(service, account);
		q[(__bridge id)kSecReturnData] = @YES;
		q[(__bridge id)kSecMatchLimit] = (__bridge id)kSecMatchLimitOne;
		CFTypeRef result = NULL;
		OSStatus status = SecItemCopyMatching((__bridge CFDictionaryRef)q, &result);
		if (status != errSecSuccess) return status;
		NSData *data = (__bridge_transfer NSData *)result;
		*outLen = (int)[data length];
		*out = malloc([data length] + 1);
		memcpy(*out, [data bytes], [data length]);
		return errSecSuccess;
	}
}

static OSStatus edrive_kc_exists(const char *service, const char *account) {
	@autoreleasepool {
		NSMutableDictionary *q = edrive_query(service, account);
		q[(__bridge id)kSecMatchLimit] = (__bridge id)kSecMatchLimitOne;
		return SecItemCopyMatching((__bridge CFDictionaryRef)q, NULL);
	}
}

// Items are created by this process without an explicit access list, so the
// Keychain trusts only the edrive binary that created them. Other programs,
// including /usr/bin/security, get a Keychain password prompt instead.
static OSStatus edrive_kc_set(const char *service, const char *account, const char *label, const void *value, int valueLen) {
	@autoreleasepool {
		NSData *data = [NSData dataWithBytes:value length:valueLen];
		NSMutableDictionary *q = edrive_query(service, account);
		OSStatus status = SecItemUpdate((__bridge CFDictionaryRef)q,
			(__bridge CFDictionaryRef)@{(__bridge id)kSecValueData: data});
		if (status != errSecItemNotFound) return status;
		q[(__bridge id)kSecValueData] = data;
		q[(__bridge id)kSecAttrLabel] = [NSString stringWithUTF8String:label];
		return SecItemAdd((__bridge CFDictionaryRef)q, NULL);
	}
}

static OSStatus edrive_kc_delete(const char *service, const char *account) {
	@autoreleasepool {
		return SecItemDelete((__bridge CFDictionaryRef)edrive_query(service, account));
	}
}

static int edrive_screen_locked(void) {
	CFDictionaryRef session = CGSessionCopyCurrentDictionary();
	if (session == NULL) return 0;
	int locked = 0;
	CFBooleanRef value = CFDictionaryGetValue(session, CFSTR("CGSSessionScreenIsLocked"));
	if (value != NULL && CFGetTypeID(value) == CFBooleanGetTypeID()) {
		locked = CFBooleanGetValue(value) ? 1 : 0;
	}
	CFRelease(session);
	return locked;
}

static long edrive_clipboard_set(const char *value) {
	@autoreleasepool {
		NSPasteboard *pb = [NSPasteboard generalPasteboard];
		[pb clearContents];
		// nspasteboard.org markers: clipboard managers skip concealed/transient items.
		[pb declareTypes:@[NSPasteboardTypeString, @"org.nspasteboard.ConcealedType", @"org.nspasteboard.TransientType"] owner:nil];
		[pb setString:[NSString stringWithUTF8String:value] forType:NSPasteboardTypeString];
		[pb setString:@"" forType:@"org.nspasteboard.ConcealedType"];
		[pb setString:@"" forType:@"org.nspasteboard.TransientType"];
		return (long)[pb changeCount];
	}
}

static void edrive_clipboard_clear_if(long changeCount) {
	@autoreleasepool {
		NSPasteboard *pb = [NSPasteboard generalPasteboard];
		if ((long)[pb changeCount] == changeCount) {
			[pb clearContents];
		}
	}
}
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

// ErrNotFound is returned when a Keychain item does not exist.
var ErrNotFound = errors.New("keychain item not found")

// ErrCancelled is returned when the user dismisses the Touch ID prompt.
var ErrCancelled = errors.New("authentication cancelled")

const errSecItemNotFound = -25300

// Authenticate asks for Touch ID, falling back to the Mac login password.
// macOS shows the reason as: "edrive is trying to <reason>."
func Authenticate(reason string) error {
	cReason := C.CString(reason)
	defer C.free(unsafe.Pointer(cReason))
	var cErr *C.char
	code := C.edrive_authenticate(cReason, &cErr)
	detail := ""
	if cErr != nil {
		detail = C.GoString(cErr)
		C.free(unsafe.Pointer(cErr))
	}
	switch code {
	case 0:
		return nil
	case 3:
		return ErrCancelled
	case 2:
		return fmt.Errorf("Touch ID / password authentication is unavailable here (%s); edrive must run in your logged-in macOS session", detail)
	default:
		return fmt.Errorf("authentication failed: %s", detail)
	}
}

// CanAuthenticate reports whether Touch ID or the login password can be used.
func CanAuthenticate() bool {
	return C.edrive_can_authenticate() == 1
}

func KeychainGet(service, account string) ([]byte, error) {
	cService, cAccount := C.CString(service), C.CString(account)
	defer C.free(unsafe.Pointer(cService))
	defer C.free(unsafe.Pointer(cAccount))
	var out unsafe.Pointer
	var length C.int
	status := C.edrive_kc_get(cService, cAccount, &out, &length)
	if status == errSecItemNotFound {
		return nil, ErrNotFound
	}
	if status != 0 {
		return nil, fmt.Errorf("read Keychain item %s/%s: OSStatus %d", service, account, int(status))
	}
	defer C.free(out)
	value := C.GoBytes(out, length)
	// Wipe the C copy before freeing it.
	C.memset(out, 0, C.size_t(length))
	return value, nil
}

// KeychainExists checks for an item without reading its secret, so it never prompts.
func KeychainExists(service, account string) bool {
	cService, cAccount := C.CString(service), C.CString(account)
	defer C.free(unsafe.Pointer(cService))
	defer C.free(unsafe.Pointer(cAccount))
	return C.edrive_kc_exists(cService, cAccount) == 0
}

func KeychainSet(service, account, label string, value []byte) error {
	if len(value) == 0 {
		return errors.New("refusing to store an empty Keychain item")
	}
	cService, cAccount, cLabel := C.CString(service), C.CString(account), C.CString(label)
	defer C.free(unsafe.Pointer(cService))
	defer C.free(unsafe.Pointer(cAccount))
	defer C.free(unsafe.Pointer(cLabel))
	status := C.edrive_kc_set(cService, cAccount, cLabel, unsafe.Pointer(&value[0]), C.int(len(value)))
	if status != 0 {
		return fmt.Errorf("store Keychain item %s/%s: OSStatus %d", service, account, int(status))
	}
	return nil
}

func KeychainDelete(service, account string) error {
	cService, cAccount := C.CString(service), C.CString(account)
	defer C.free(unsafe.Pointer(cService))
	defer C.free(unsafe.Pointer(cAccount))
	status := C.edrive_kc_delete(cService, cAccount)
	if status != 0 && status != errSecItemNotFound {
		return fmt.Errorf("delete Keychain item %s/%s: OSStatus %d", service, account, int(status))
	}
	return nil
}

func ScreenLocked() bool {
	return C.edrive_screen_locked() == 1
}

// ClipboardSet copies value and returns the pasteboard change count, which
// ClipboardClearIf uses to avoid wiping something the user copied later.
func ClipboardSet(value string) int64 {
	cValue := C.CString(value)
	defer C.free(unsafe.Pointer(cValue))
	defer C.memset(unsafe.Pointer(cValue), 0, C.size_t(len(value)))
	return int64(C.edrive_clipboard_set(cValue))
}

func ClipboardClearIf(changeCount int64) {
	C.edrive_clipboard_clear_if(C.long(changeCount))
}
