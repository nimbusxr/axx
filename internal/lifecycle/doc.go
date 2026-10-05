// Package lifecycle starts and stops the applications under test.
//
// A [Manager] launches the services declared under `services:` in axx.yaml, streams
// their output with an `[<app>] ` prefix, waits until each one is ready
// (http, tcp, exec and log checks) and, at the end of the run, stops them in
// reverse order and runs their cleanup commands.
//
// Services start in declaration order, one after another, unless some service
// declares dependsOn: then the dependency graph decides and services whose
// dependencies are ready start concurrently.
//
// Every service runs in its own process group (a Job Object on Windows), so
// stopping a service also stops everything it spawned. Cleanup always runs
// after a service stops, even when it crashed or never became ready.
//
// Developers can keep a service out of axx's hands (Options.Attach: axx only
// waits for it to be ready), hand it to their IDE's debugger (Options.Debug)
// or skip the lifecycle entirely (Options.NoStart). A state file lets
// `axx down` reap services left behind by a run that was killed (see [Reap]).
package lifecycle
