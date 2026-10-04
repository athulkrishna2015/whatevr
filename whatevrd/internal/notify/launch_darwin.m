#import <AppKit/AppKit.h>
#import <Foundation/Foundation.h>
#include <stdlib.h>
#include <string.h>
char *launch_notification_app(const char *path) {
 @autoreleasepool {
  NSURL *url = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path]];
  NSWorkspaceOpenConfiguration *config = [NSWorkspaceOpenConfiguration configuration];
  config.activates = NO;
  dispatch_semaphore_t done = dispatch_semaphore_create(0);
  // __block Objective-C storage survives a timeout until the completion runs.
  __block NSString *failure = nil;
  [[NSWorkspace sharedWorkspace] openApplicationAtURL:url configuration:config completionHandler:^(NSRunningApplication *app, NSError *error) {
   failure = error ? [error.localizedDescription copy] : nil;
   dispatch_semaphore_signal(done);
  }];
  if (dispatch_semaphore_wait(done, dispatch_time(DISPATCH_TIME_NOW, 5*NSEC_PER_SEC))) return strdup("timed out launching notification helper");
  return failure ? strdup(failure.UTF8String) : NULL;
 }
}
