# Changelog

## [0.2.0](https://github.com/nimbusxr/axx/compare/intellij-v0.1.5...intellij-v0.2.0) (2026-10-05)


### ⚠ BREAKING CHANGES

* desktop apps through the accessibility tree, and one vocabulary for every app ([#104](https://github.com/nimbusxr/axx/issues/104))

### Features

* desktop apps through the accessibility tree, and one vocabulary for every app ([#104](https://github.com/nimbusxr/axx/issues/104)) ([6169326](https://github.com/nimbusxr/axx/commit/6169326c9db4eb3f5b728ac7a03e7c40ad96eb76))

## [0.1.5](https://github.com/nimbusxr/axx/compare/intellij-v0.1.4...intellij-v0.1.5) (2026-10-01)


### Features

* **intellij:** IntelliJ IDEA 2026.1.4 or later, with the LSP API's current names ([#80](https://github.com/nimbusxr/axx/issues/80)) ([e15b5bf](https://github.com/nimbusxr/axx/commit/e15b5bf321fd846b1efeb6d76c91a54ed970a1b2))

## [0.1.4](https://github.com/nimbusxr/axx/compare/intellij-v0.1.3...intellij-v0.1.4) (2026-10-01)


### Features

* **intellij:** a Profiles list in the axx run configuration; the template's profiles start runs from the gutter, the editor and the project view ([11688a1](https://github.com/nimbusxr/axx/commit/11688a131376de7784d851eb7b27801c652b9b4d))
* **vscode:** the gear next to Run, Debug and Watch picks the profiles they apply (axx.profiles) ([11688a1](https://github.com/nimbusxr/axx/commit/11688a131376de7784d851eb7b27801c652b9b4d))

## [0.1.3](https://github.com/nimbusxr/axx/compare/intellij-v0.1.2...intellij-v0.1.3) (2026-10-01)


### Bug Fixes

* **intellij:** hide Cucumber+'s undefined step warnings in axx projects ([#74](https://github.com/nimbusxr/axx/issues/74)) ([f584dd2](https://github.com/nimbusxr/axx/commit/f584dd2ae8d70ade681bc6bfe9ba00147caf9941))

## [0.1.2](https://github.com/nimbusxr/axx/compare/intellij-v0.1.1...intellij-v0.1.2) (2026-10-01)


### Features

* axx run --watch shows what scenarios do, browsers and devices alike, one at a time (run.watch, run.slowdown) ([bc154bf](https://github.com/nimbusxr/axx/commit/bc154bf554bde7e229b77cb21cc2803ccf2b8adc))
* **core:** Scenario.Hold stops a step's timeout while it waits on what the scenarios share ([4d4598d](https://github.com/nimbusxr/axx/commit/4d4598d5ddb00a4f6d8e8106209c9cb6be746143))
* **core:** Suite.Watching tells packs a person watches the run ([bc154bf](https://github.com/nimbusxr/axx/commit/bc154bf554bde7e229b77cb21cc2803ccf2b8adc))
* **intellij:** the axx wordmark as the plugin's icon ([bf937f8](https://github.com/nimbusxr/axx/commit/bf937f87e3e7ff0118ba68989a56f2a703d8da98))
* **intellij:** Watch watches devices too ([bc154bf](https://github.com/nimbusxr/axx/commit/bc154bf554bde7e229b77cb21cc2803ccf2b8adc))
* **mobile-android:** emulators axx starts open Chrome without its first-run screens or prompts ([bc154bf](https://github.com/nimbusxr/axx/commit/bc154bf554bde7e229b77cb21cc2803ccf2b8adc))
* **mobile-android:** the host ports row: ports of this machine the app reaches as localhost ([bc154bf](https://github.com/nimbusxr/axx/commit/bc154bf554bde7e229b77cb21cc2803ccf2b8adc))
* **mobile-ios:** a watched run's simulators show in Device Hub ([bc154bf](https://github.com/nimbusxr/axx/commit/bc154bf554bde7e229b77cb21cc2803ccf2b8adc))
* **mobile-ios:** packs.mobile-ios.keep keeps simulators booted for the next run ([bc154bf](https://github.com/nimbusxr/axx/commit/bc154bf554bde7e229b77cb21cc2803ccf2b8adc))
* **mobile:** a watched run pauses after each mobile action ([bc154bf](https://github.com/nimbusxr/axx/commit/bc154bf554bde7e229b77cb21cc2803ccf2b8adc))
* **vscode:** Watch watches devices too ([bc154bf](https://github.com/nimbusxr/axx/commit/bc154bf554bde7e229b77cb21cc2803ccf2b8adc))


### Bug Fixes

* a feature file named on the command line or in an editor runs whatever run.tags leaves out of a whole run ([4d4598d](https://github.com/nimbusxr/axx/commit/4d4598d5ddb00a4f6d8e8106209c9cb6be746143))
* a file:line outside every scenario is an error (AXX-E0204), not a run of nothing ([4d4598d](https://github.com/nimbusxr/axx/commit/4d4598d5ddb00a4f6d8e8106209c9cb6be746143))
* a run cleans up what a run stopped by force left behind, as axx down does ([4d4598d](https://github.com/nimbusxr/axx/commit/4d4598d5ddb00a4f6d8e8106209c9cb6be746143))
* axx processes preparing the same packs share one build instead of breaking each other's ([4d4598d](https://github.com/nimbusxr/axx/commit/4d4598d5ddb00a4f6d8e8106209c9cb6be746143))
* **intellij:** a relative axx executable resolves from the project for runs too ([4d4598d](https://github.com/nimbusxr/axx/commit/4d4598d5ddb00a4f6d8e8106209c9cb6be746143))
* **mobile-android:** other apps' error and not-responding dialogs stay off the app ([4d4598d](https://github.com/nimbusxr/axx/commit/4d4598d5ddb00a4f6d8e8106209c9cb6be746143))
* **mobile-ios:** Device Hub (or Simulator) stays open while scenarios run ([4d4598d](https://github.com/nimbusxr/axx/commit/4d4598d5ddb00a4f6d8e8106209c9cb6be746143))
* **mobile-ios:** list items are found on iOS 27 ([4d4598d](https://github.com/nimbusxr/axx/commit/4d4598d5ddb00a4f6d8e8106209c9cb6be746143))
* **mobile:** waiting for a device does not count against the step timeout ([4d4598d](https://github.com/nimbusxr/axx/commit/4d4598d5ddb00a4f6d8e8106209c9cb6be746143))


### Performance Improvements

* **mobile-ios:** WebDriverAgent starts once per simulator, not once per scenario ([bc154bf](https://github.com/nimbusxr/axx/commit/bc154bf554bde7e229b77cb21cc2803ccf2b8adc))

## [0.1.1](https://github.com/nimbusxr/axx/compare/intellij-v0.1.0...intellij-v0.1.1) (2026-09-27)


### Features

* web and files packs, with Playwright's tools speaking steps ([#3](https://github.com/nimbusxr/axx/issues/3)) ([89c3bb7](https://github.com/nimbusxr/axx/commit/89c3bb7c7cd50ee7badca796ac24261233130f56))

## 0.1.0 (2026-09-25)


### Features

* init commit ([63bf642](https://github.com/nimbusxr/axx/commit/63bf642d25367fbb7c5a0d19c3f877b54fae4ba1))
