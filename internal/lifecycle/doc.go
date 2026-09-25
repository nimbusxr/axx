// Package lifecycle starts and stops the applications under test.
//
// A [Manager] launches the apps declared under `apps:` in axx.yaml, streams
// their output with an `[<app>] ` prefix, waits until each one is ready
// (http, tcp, exec and log checks) and, at the end of the run, stops them in
// reverse order and runs their cleanup commands.
//
// Apps start in declaration order, one after another, unless some app
// declares dependsOn: then the dependency graph decides and apps whose
// dependencies are ready start concurrently.
//
// Every app runs in its own process group (a Job Object on Windows), so
// stopping an app also stops everything it spawned. Cleanup always runs
// after an app stops, even when it crashed or never became ready.
//
// Developers can keep an app out of axx's hands (Options.Attach: axx only
// waits for it to be ready), hand it to their IDE's debugger (Options.Debug)
// or skip the lifecycle entirely (Options.NoStart). A state file lets
// `axx down` reap apps left behind by a run that was killed (see [Reap]).
package lifecycle
