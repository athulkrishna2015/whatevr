#import <AppKit/AppKit.h>
#import <Foundation/Foundation.h>
#include <stdlib.h>
#include <string.h>
static NSString *field(NSBundle *b, NSString *key) {
 id v = [b objectForInfoDictionaryKey:key];
 if (![v isKindOfClass:[NSString class]]) return nil;
 NSString *s = v;
 // records are tab and newline separated
 if ([s rangeOfCharacterFromSet:[NSCharacterSet characterSetWithCharactersInString:@"\t\n"]].location != NSNotFound) return nil;
 return s;
}
char *frontend_apps(void) {
 @autoreleasepool {
  NSMutableString *out = [NSMutableString string];
  if (@available(macOS 12.0, *)) {
   NSURL *probe = [NSURL URLWithString:@"whatevr-frontend:"];
   for (NSURL *app in [[NSWorkspace sharedWorkspace] URLsForApplicationsToOpenURL:probe]) {
    NSBundle *b = [NSBundle bundleWithURL:app];
    NSString *ident = field(b, @"WhatevrFrontendID");
    NSString *name = field(b, @"CFBundleDisplayName") ?: field(b, @"CFBundleName") ?: @"";
    NSString *path = app.path;
    if (!ident || [path rangeOfCharacterFromSet:[NSCharacterSet characterSetWithCharactersInString:@"\t\n"]].location != NSNotFound) continue;
    [out appendFormat:@"%@\t%@\t%@\n", path, ident, name];
   }
  }
  return strdup(out.UTF8String);
 }
}
char *open_frontend_app(const char *path) {
 @autoreleasepool {
  NSURL *url = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path]];
  NSWorkspaceOpenConfiguration *config = [NSWorkspaceOpenConfiguration configuration];
  config.activates = YES;
  dispatch_semaphore_t done = dispatch_semaphore_create(0);
  __block NSString *failure = nil;
  [[NSWorkspace sharedWorkspace] openApplicationAtURL:url configuration:config completionHandler:^(NSRunningApplication *app, NSError *error) {
   failure = error ? [error.localizedDescription copy] : nil;
   dispatch_semaphore_signal(done);
  }];
  if (dispatch_semaphore_wait(done, dispatch_time(DISPATCH_TIME_NOW, 10*NSEC_PER_SEC))) return strdup("timed out opening the frontend");
  return failure ? strdup(failure.UTF8String) : NULL;
 }
}
