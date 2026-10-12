// SPDX-License-Identifier: Apache-2.0

// axx loads this library into the iOS simulators it boots (springboard.go). In SpringBoard, it
// gives Cover Sheet, which shows Notification Center, the proximity sensor an iPhone has and a
// simulator lacks. Cover Sheet lets a person hold a notification there, to see its actions,
// only on a phone it can tell is in a hand: one with a proximity sensor, or with Face ID
// unlock, which takes a passcode a simulator cannot have. Without a sensor, a hold is taken
// for a tap. Everywhere else (the app under test, WebDriverAgent) the library does nothing.

#import <Foundation/Foundation.h>
#import <objc/runtime.h>

// report writes how it went where axx reads it: a file named for the simulator, in the folder
// axx gives, with SpringBoard's process id first.
static void report(NSString *status) {
  const char *dir = getenv("AXX_SPRINGBOARD_STATUS");
  const char *udid = getenv("SIMULATOR_UDID");
  if (dir == NULL || udid == NULL) {
    return;
  }
  NSString *line = [NSString stringWithFormat:@"%d %@\n", getpid(), status];
  [line writeToFile:[NSString stringWithFormat:@"%s/%s", dir, udid] atomically:YES encoding:NSUTF8StringEncoding error:nil];
}

__attribute__((constructor)) static void proximitySensor(void) {
  if (![[[NSProcessInfo processInfo] processName] isEqualToString:@"SpringBoard"]) {
    return;
  }
  Class list = NSClassFromString(@"CSCombinedListViewController");
  Ivar sensor = list == Nil ? NULL : class_getInstanceVariable(list, "_hasProximitySensor");
  SEL hold = NSSelectorFromString(@"notificationStructuredListViewControllerShouldAllowLongPressGesture:");
  SEL interact = NSSelectorFromString(@"notificationStructuredListViewController:shouldAllowInteractionsForNotificationRequest:");
  Method holdMethod = list == Nil ? NULL : class_getInstanceMethod(list, hold);
  Method interactMethod = list == Nil ? NULL : class_getInstanceMethod(list, interact);
  if (sensor == NULL || holdMethod == NULL || interactMethod == NULL) {
    report(@"this iOS's Cover Sheet has no -[CSCombinedListViewController _hasProximitySensor] to give a proximity sensor");
    return;
  }
  ptrdiff_t offset = ivar_getOffset(sensor);
  // Cover Sheet reads the sensor as it decides: it has it by then.
  BOOL (*holdOriginal)(id, SEL, id) = (BOOL (*)(id, SEL, id))method_getImplementation(holdMethod);
  method_setImplementation(holdMethod, imp_implementationWithBlock(^BOOL(id self, id controller) {
    *(BOOL *)((__bridge void *)self + offset) = YES;
    return holdOriginal(self, hold, controller);
  }));
  BOOL (*interactOriginal)(id, SEL, id, id) = (BOOL (*)(id, SEL, id, id))method_getImplementation(interactMethod);
  method_setImplementation(interactMethod, imp_implementationWithBlock(^BOOL(id self, id controller, id request) {
    *(BOOL *)((__bridge void *)self + offset) = YES;
    return interactOriginal(self, interact, controller, request);
  }));
  report(@"ok");
}
