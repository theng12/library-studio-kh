#import <AppKit/AppKit.h>
#include <stdlib.h>
#include <string.h>

// One native directory panel for the Go sidecar. Returns a malloc'd path the
// Go caller frees, or NULL when the owner cancels.
char *libraryPickFolder(const char *prompt) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
        NSOpenPanel *panel = [NSOpenPanel openPanel];
        [panel setCanChooseFiles:NO];
        [panel setCanChooseDirectories:YES];
        [panel setAllowsMultipleSelection:NO];
        [panel setCanCreateDirectories:YES];
        [panel setTitle:@"Choose a folder"];
        if (prompt != NULL && prompt[0] != '\0') {
            [panel setMessage:[NSString stringWithUTF8String:prompt]];
        }
        // The sidecar is a background child process; without this its panel can
        // open behind the owner's window.
        [NSApp activateIgnoringOtherApps:YES];
        if ([panel runModal] != NSModalResponseOK) {
            return NULL;
        }
        NSURL *url = [panel URL];
        if (url == nil || [url path] == nil) {
            return NULL;
        }
        const char *path = [[url path] UTF8String];
        return path == NULL ? NULL : strdup(path);
    }
}
