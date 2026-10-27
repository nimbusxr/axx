// SPDX-License-Identifier: Apache-2.0

// axx loads this library into the iOS simulators it boots (springboard.go). In an app, this part
// gives the app's background tasks what an iPhone gives them, and a simulator does not: iOS on a
// simulator refuses every request to schedule one ("BGTaskScheduler is not available on this
// platform"), so a task never runs there, and its handler cannot be tested. Here:
//
// - The app's requests are kept, as an iPhone keeps them until the task runs or the app cancels
//   it: scheduling succeeds, and the app's pending requests list them.
// - When axx asks, iOS runs a task now, as Xcode's debugger command does on an iPhone
//   (-[BGTaskScheduler _simulateLaunchForTaskWithIdentifier:]): the task the app scheduled is
//   made, its request used up, and the task handed to the handler the app registered, on its
//   queue. The app's own code runs, and says when it is done (setTaskCompleted).
//
// In SpringBoard, it wakes the app for the task, as iOS does: an app in the background is
// suspended, and runs again only while iOS lets it. It holds the app awake until the task is
// done, or 30 seconds, as long as iOS gives an app refresh task.
//
// axx asks with a request file and a notification (background.go): the request names the app,
// its process and the task. SpringBoard holds the app awake, then tells it; the app answers in a
// result file, and says in another when the task is done.

#import <Foundation/Foundation.h>
#import <notify.h>
#import <stdlib.h>
#import <string.h>
#import <objc/message.h>
#import <objc/runtime.h>

// axx asks SpringBoard; SpringBoard, holding the app awake, asks the app; the app says when the
// task is done. The app runs the task only once it is held awake: told at the same time as
// SpringBoard, it could start before SpringBoard held it (SpringBoard busy, on a busy Mac), and
// iOS suspended it halfway.
static NSString *const runNotification = @"com.nimbusxr.axx.background-task";
static NSString *const appNotification = @"com.nimbusxr.axx.background-task.app";
static NSString *const doneNotification = @"com.nimbusxr.axx.background-task.done";

// The request and result files, in the folder axx gives (AXX_SPRINGBOARD_STATUS), named for the
// simulator: nil when axx did not start the simulator's processes with them.
static NSString *axxFile(NSString *suffix) {
  const char *dir = getenv("AXX_SPRINGBOARD_STATUS");
  const char *udid = getenv("SIMULATOR_UDID");
  if (dir == NULL || udid == NULL) {
    return nil;
  }
  return [NSString stringWithFormat:@"%s/%s.%@", dir, udid, suffix];
}

// A request: its id, the app's bundle identifier and process, and the task's identifier.
typedef struct {
  NSString *nonce, *bundle, *task;
  int pid;
} request;

static BOOL readRequest(request *r) {
  NSString *path = axxFile(@"task");
  NSString *text = path == nil ? nil : [NSString stringWithContentsOfFile:path encoding:NSUTF8StringEncoding error:nil];
  NSArray<NSString *> *lines = [text componentsSeparatedByString:@"\n"];
  if (lines.count < 4) {
    return NO;
  }
  *r = (request){lines[0], lines[1], lines[3], lines[2].intValue};
  return YES;
}

// send sends a message that answers an object.
static id send(id target, NSString *selector) {
  return ((id (*)(id, SEL))objc_msgSend)(target, NSSelectorFromString(selector));
}

// MARK: - In the app

// The app's scheduled tasks: each task's identifier, and its request's class. They are kept in the
// app's temporary folder, as an iPhone keeps them across the app's launches; a new install starts
// with none.
static NSMutableDictionary<NSString *, NSString *> *scheduled;

// The tasks axx ran, by identifier: each says when the app has said it is done (its _completed,
// which setTaskCompleted sets).
static NSMutableDictionary<NSString *, id> *ran;

static NSString *scheduledPath(void) {
  return [NSTemporaryDirectory() stringByAppendingPathComponent:@"axx-background-tasks.plist"];
}

static void saveScheduled(void) {
  [scheduled writeToFile:scheduledPath() atomically:YES];
}

// replace gives a method of a class a new implementation, which gets the old one.
static IMP replace(Class c, NSString *name, id (^make)(IMP original)) {
  Method m = class_getInstanceMethod(c, NSSelectorFromString(name));
  if (m == NULL) {
    return NULL;
  }
  IMP original = method_getImplementation(m);
  method_setImplementation(m, imp_implementationWithBlock(make(original)));
  return original;
}

// keepRequests has the scheduler keep the app's requests, as on an iPhone. It takes them where the
// app hands them over, iOS's public methods (the private ones behind them differ from one iOS to
// the next). A request iOS would refuse anyway (a task the app registered no handler for, or not in
// its Info.plist) is refused as iOS refuses it.
static BOOL kept(id scheduler, id taskRequest) {
  NSString *identifier = send(taskRequest, @"identifier");
  Class taskClass = ((Class (*)(id, SEL))objc_msgSend)([taskRequest class], NSSelectorFromString(@"_correspondingTaskClass"));
  NSDictionary *registrations = send(scheduler, @"_registrations");
  if (identifier == nil || taskClass == Nil || registrations[identifier] == nil) {
    return NO;
  }
  @synchronized(scheduled) {
    scheduled[identifier] = NSStringFromClass([taskRequest class]);
    saveScheduled();
  }
  return YES;
}

static void keepRequests(Class schedulerClass) {
  SEL submitSel = NSSelectorFromString(@"submitTaskRequest:error:");
  replace(schedulerClass, @"submitTaskRequest:error:", ^id(IMP original) {
    return ^BOOL(id self, id taskRequest, NSError **error) {
      if (kept(self, taskRequest)) {
        return YES;
      }
      return ((BOOL (*)(id, SEL, id, NSError **))original)(self, submitSel, taskRequest, error);
    };
  });
  SEL submitAsyncSel = NSSelectorFromString(@"submitTaskRequest:completionHandler:");
  replace(schedulerClass, @"submitTaskRequest:completionHandler:", ^id(IMP original) {
    return ^(id self, id taskRequest, void (^done)(NSError *)) {
      if (kept(self, taskRequest)) {
        if (done != nil) {
          done(nil);
        }
        return;
      }
      ((void (*)(id, SEL, id, void (^)(NSError *)))original)(self, submitAsyncSel, taskRequest, done);
    };
  });
  replace(schedulerClass, @"getPendingTaskRequestsWithCompletionHandler:", ^id(IMP original) {
    return ^(id self, void (^handler)(NSArray *)) {
      NSMutableArray *requests = [NSMutableArray array];
      @synchronized(scheduled) {
        [scheduled enumerateKeysAndObjectsUsingBlock:^(NSString *identifier, NSString *className, BOOL *stop) {
          id r = ((id (*)(id, SEL, id))objc_msgSend)([NSClassFromString(className) alloc], NSSelectorFromString(@"initWithIdentifier:"), identifier);
          if (r != nil) {
            [requests addObject:r];
          }
        }];
      }
      dispatch_async(dispatch_get_global_queue(QOS_CLASS_UTILITY, 0), ^{
        handler(requests);
      });
    };
  });
  SEL cancelSel = NSSelectorFromString(@"cancelTaskRequestWithIdentifier:");
  replace(schedulerClass, @"cancelTaskRequestWithIdentifier:", ^id(IMP original) {
    return ^(id self, NSString *identifier) {
      @synchronized(scheduled) {
        [scheduled removeObjectForKey:identifier];
        saveScheduled();
      }
      ((void (*)(id, SEL, id))original)(self, cancelSel, identifier);
    };
  });
  SEL cancelAllSel = NSSelectorFromString(@"cancelAllTaskRequests");
  replace(schedulerClass, @"cancelAllTaskRequests", ^id(IMP original) {
    return ^(id self) {
      @synchronized(scheduled) {
        [scheduled removeAllObjects];
        saveScheduled();
      }
      ((void (*)(id, SEL))original)(self, cancelAllSel);
    };
  });
  // A task iOS runs on an iPhone carries what the system scheduled; one the debugger has run
  // carries nothing. The simulator's tasks lack even the means to say so.
  Class task = NSClassFromString(@"BGTask");
  SEL activity = NSSelectorFromString(@"_activity");
  if (task != Nil && !class_respondsToSelector(task, activity)) {
    class_addMethod(task, activity, imp_implementationWithBlock(^id(id self) { return nil; }), "@@:");
  }
}

// unsupported names the first of the methods axx runs a task with that this iOS lacks: nil when it
// has them all.
static NSString *unsupported(void) {
  NSArray<NSArray<NSString *> *> *needed = @[
    @[ @"BGTaskScheduler", @"submitTaskRequest:error:" ],
    @[ @"BGTaskScheduler", @"_registrations" ],
    @[ @"BGTaskScheduler", @"_runningTasksMap" ],
    @[ @"BGTaskScheduler", @"_runTask:registration:" ],
    @[ @"BGAppRefreshTask", @"_initWithIdentifier:activity:" ],
    @[ @"BGTask", @"_completed" ],
  ];
  for (NSArray<NSString *> *m in needed) {
    Class c = NSClassFromString(m[0]);
    if (c == Nil || !class_respondsToSelector(c, NSSelectorFromString(m[1]))) {
      return [NSString stringWithFormat:@"-[%@ %@]", m[0], m[1]];
    }
  }
  if (!class_respondsToSelector(object_getClass(NSClassFromString(@"BGAppRefreshTaskRequest")), NSSelectorFromString(@"_correspondingTaskClass"))) {
    return @"+[BGAppRefreshTaskRequest _correspondingTaskClass]";
  }
  return nil;
}

// runTask runs a task the app scheduled, as the debugger command does on an iPhone, and says how
// it went: "ran", "not scheduled", "not registered" or "running".
static NSString *runTask(NSString *identifier) {
  id scheduler = send(NSClassFromString(@"BGTaskScheduler"), @"sharedScheduler");
  NSString *requestClass;
  @synchronized(scheduled) {
    requestClass = scheduled[identifier];
  }
  if (requestClass == nil) {
    return @"not scheduled";
  }
  id registration = ((NSDictionary *)send(scheduler, @"_registrations"))[identifier];
  if (registration == nil) {
    return @"not registered";
  }
  if ([(NSMapTable *)send(scheduler, @"_runningTasksMap") objectForKey:identifier] != nil) {
    return @"running";
  }
  Class taskClass = ((Class (*)(id, SEL))objc_msgSend)(NSClassFromString(requestClass), NSSelectorFromString(@"_correspondingTaskClass"));
  id task = ((id (*)(id, SEL, id, id))objc_msgSend)([taskClass alloc], NSSelectorFromString(@"_initWithIdentifier:activity:"), identifier, nil);
  if (task == nil) {
    return @"not scheduled";
  }
  @synchronized(ran) {
    ran[identifier] = task;
  }
  ((void (*)(id, SEL, id))objc_msgSend)(scheduler, NSSelectorFromString(@"cancelTaskRequestWithIdentifier:"), identifier);
  ((void (*)(id, SEL, id, id))objc_msgSend)(scheduler, NSSelectorFromString(@"_runTask:registration:"), task, registration);
  return @"ran";
}

// whenDone says the task is done, once the app said so (setTaskCompleted), or that it was not,
// 30 seconds on, as long as iOS gives an app refresh task: to axx, in a file next to the answer,
// and to SpringBoard, which lets the app sleep again.
// It goes by the task's own word (its _completed), not by the scheduler's list of running tasks:
// on a busy Mac, the task can be missing from that list for a moment after it started, and a task
// taken for done then had SpringBoard let the app sleep before it finished.
static void whenDone(NSString *nonce, NSString *identifier, int left) {
  id task;
  @synchronized(ran) {
    task = ran[identifier];
  }
  BOOL done = task != nil && ((BOOL (*)(id, SEL))objc_msgSend)(task, NSSelectorFromString(@"_completed"));
  if (left > 0 && !done) {
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, NSEC_PER_SEC / 4), dispatch_get_main_queue(), ^{
      whenDone(nonce, identifier, left - 1);
    });
    return;
  }
  NSString *how = done ? @"done" : @"unfinished";
  [[NSString stringWithFormat:@"%@ %@\n", nonce, how] writeToFile:axxFile(@"task-done") atomically:YES encoding:NSUTF8StringEncoding error:nil];
  notify_post(doneNotification.UTF8String);
}

static int appToken;

static void inApp(void) {
  Class schedulerClass = NSClassFromString(@"BGTaskScheduler");
  NSString *bundle = [NSBundle mainBundle].bundleIdentifier;
  if (schedulerClass == Nil || bundle == nil || axxFile(@"task") == nil) {
    return;
  }
  scheduled = [NSMutableDictionary dictionaryWithContentsOfFile:scheduledPath()] ?: [NSMutableDictionary dictionary];
  ran = [NSMutableDictionary dictionary];
  NSString *missing = unsupported();
  if (missing == nil) {
    keepRequests(schedulerClass);
  }
  notify_register_dispatch(appNotification.UTF8String, &appToken, dispatch_get_main_queue(), ^(int token) {
    request r;
    if (!readRequest(&r) || r.pid != getpid() || ![r.bundle isEqualToString:bundle]) {
      return;
    }
    NSString *status = missing == nil ? runTask(r.task) : [@"unsupported " stringByAppendingString:missing];
    [[NSString stringWithFormat:@"%@ %@\n", r.nonce, status] writeToFile:axxFile(@"task-result") atomically:YES encoding:NSUTF8StringEncoding error:nil];
    if ([status isEqualToString:@"ran"]) {
      whenDone(r.nonce, r.task, 120);
    } else {
      notify_post(doneNotification.UTF8String);
    }
  });
}

// MARK: - In SpringBoard

static int springboardToken, springboardDoneToken;
static id awake; // the assertion that holds the app awake
static id role;  // the assertion that gives it the role of an app on screen
static NSUInteger awakeFor; // which request it is for

static void letSleep(void) {
  if (awake != nil) {
    ((void (*)(id, SEL))objc_msgSend)(awake, NSSelectorFromString(@"invalidate"));
    awake = nil;
  }
  if (role != nil) {
    ((void (*)(id, SEL))objc_msgSend)(role, NSSelectorFromString(@"invalidate"));
    role = nil;
  }
}

// screenRole gives the app, for as long as the task runs, the role of an app on screen
// (UserInteractiveNonFocal), which RunningBoard grants SpringBoard (com.apple.underlying's
// UnderlyingDarwinRoleUI). iOS runs a task at background priority, which a phone, never short of
// a processor for its app, gets through at once; a simulator on a busy Mac (several simulators
// booting, a small CI machine) starves it: it took the app half a minute only to post a
// notification. The app stays in the background, and nothing on the screen changes.
static id screenRole(int pid) {
  Class target = NSClassFromString(@"RBSTarget"), attribute = NSClassFromString(@"RBSDomainAttribute"), assertion = NSClassFromString(@"RBSAssertion");
  if (target == Nil || attribute == Nil || assertion == Nil) {
    return nil;
  }
  id t = ((id (*)(id, SEL, int))objc_msgSend)(target, NSSelectorFromString(@"targetWithPid:"), pid);
  id a = ((id (*)(id, SEL, id, id))objc_msgSend)(attribute, NSSelectorFromString(@"attributeWithDomain:name:"), @"com.apple.underlying", @"UnderlyingDarwinRoleUI");
  id held = ((id (*)(id, SEL, id, id, id))objc_msgSend)([assertion alloc], NSSelectorFromString(@"initWithExplanation:target:attributes:"), @"axx: a background task, at an app's priority on screen", t, @[ a ]);
  NSError *err = nil;
  if (!((BOOL (*)(id, SEL, NSError **))objc_msgSend)(held, NSSelectorFromString(@"acquireWithError:"), &err)) {
    NSLog(@"[axx] cannot give the app the role of an app on screen: %@", err);
    return nil;
  }
  return held;
}

static void inSpringboard(void) {
  Class assertionClass = NSClassFromString(@"BKSProcessAssertion");
  if (axxFile(@"task") == nil) {
    return;
  }
  notify_register_dispatch(runNotification.UTF8String, &springboardToken, dispatch_get_main_queue(), ^(int token) {
    request r;
    if (!readRequest(&r) || r.pid <= 0) {
      return;
    }
    letSleep();
    // What iOS holds for an app it runs a task in: it may not be suspended, nor slowed down. An
    // iOS without the means to hold it runs the task all the same, unheld.
    if (assertionClass != Nil) {
      SEL init = NSSelectorFromString(@"initWithPID:flags:reason:name:withHandler:");
      unsigned flags = 1 /* prevent suspend */ | 2 /* prevent throttle down CPU */ | 4 /* allow idle sleep */ | 8 /* foreground resource priority */;
      unsigned finishTask = 4;
      awake = ((id (*)(id, SEL, int, unsigned, unsigned, id, id))objc_msgSend)([assertionClass alloc], init, r.pid, flags, finishTask, @"axx: a background task", nil);
    }
    role = screenRole(r.pid);
    NSUInteger mine = ++awakeFor;
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 30 * NSEC_PER_SEC), dispatch_get_main_queue(), ^{
      if (awakeFor == mine) {
        letSleep();
      }
    });
    // Held awake: now the app runs the task.
    notify_post(appNotification.UTF8String);
  });
  notify_register_dispatch(doneNotification.UTF8String, &springboardDoneToken, dispatch_get_main_queue(), ^(int token) {
    letSleep();
  });
}

// The library is in processes started with the simulator's launchd defaults: it decides with C alone
// whether a process is one it has work in, SpringBoard or an app that is not iOS's own (launchd
// names an app's job UIKitApplication:<bundle identifier>), and leaves every other one alone.
// Foundation is no help here: +[NSBundle mainBundle] opens a log, which waits on logd, and in the
// xpcproxy_sim that starts each of the simulator's services, logd's own among them, it waits
// forever, and the simulator never finishes booting.
__attribute__((constructor)) static void backgroundTasks(void) {
  if (strcmp(getprogname(), "SpringBoard") == 0) {
    inSpringboard();
    return;
  }
  const char *job = getenv("XPC_SERVICE_NAME");
  if (job != NULL && strncmp(job, "UIKitApplication:", 17) == 0 && strncmp(job + 17, "com.apple.", 10) != 0) {
    inApp();
  }
}
