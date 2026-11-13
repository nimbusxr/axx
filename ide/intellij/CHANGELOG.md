# Changelog

## [0.3.8](https://github.com/nimbusxr/axx/compare/intellij-v0.3.7...intellij-v0.3.8) (2026-11-13)


### Features

* axx doctor says whether the machine has the Java and Python the coverage packs read with ([b13cf26](https://github.com/nimbusxr/axx/commit/b13cf26980fb77097a1f93d4a64dd4dee6703b60))
* axx run --coverage measures how much of the services under test the scenarios run, and writes one lcov and Istanbul report of the whole run ([b13cf26](https://github.com/nimbusxr/axx/commit/b13cf26980fb77097a1f93d4a64dd4dee6703b60))
* axx up --coverage keeps services measured, JVM and Node.js run by run, Go and Python until axx down ([b13cf26](https://github.com/nimbusxr/axx/commit/b13cf26980fb77097a1f93d4a64dd4dee6703b60))
* coverage minimums in axx.yaml, for the whole run and per service; a run of every scenario under one exits 6, and axx coverage check checks CI's shards together ([b13cf26](https://github.com/nimbusxr/axx/commit/b13cf26980fb77097a1f93d4a64dd4dee6703b60))
* **host:** the axx host measures a run's coverage, approved as a definition of its own ([b13cf26](https://github.com/nimbusxr/axx/commit/b13cf26980fb77097a1f93d4a64dd4dee6703b60))
* **ide:** Run with Coverage in IntelliJ and VS Code shows a run's coverage in the editor, for every language ([b13cf26](https://github.com/nimbusxr/axx/commit/b13cf26980fb77097a1f93d4a64dd4dee6703b60))
* the coverage-jvm, coverage-go, coverage-python and coverage-node packs measure JVM, Go, Python and Node.js services, on the machine or in containers, and coverage-core reads the lcov or Cobertura any other service writes ([b13cf26](https://github.com/nimbusxr/axx/commit/b13cf26980fb77097a1f93d4a64dd4dee6703b60))
* **web-coverage:** in a run with coverage, the web apps' JavaScript is part of the run's report ([b13cf26](https://github.com/nimbusxr/axx/commit/b13cf26980fb77097a1f93d4a64dd4dee6703b60))

## [0.3.7](https://github.com/nimbusxr/axx/compare/intellij-v0.3.6...intellij-v0.3.7) (2026-11-12)


### Features

* every release publishes its docs as axx_&lt;version&gt;_docs.tar.gz, and axx docs serve serves them on this machine (port 7357, --port or axx.yaml's docs.port), for a network that cannot reach axx.nimbusxr.us ([1391186](https://github.com/nimbusxr/axx/commit/13911861516ebd1d84e3886d9467c36eda70c7a1))
* the links axx prints (errors' docs lines, explain, lint's SARIF, the help) lead to the docs site AXX_DOCS_URL, or axx.yaml's docs.url, names ([1391186](https://github.com/nimbusxr/axx/commit/13911861516ebd1d84e3886d9467c36eda70c7a1))

## [0.3.6](https://github.com/nimbusxr/axx/compare/intellij-v0.3.5...intellij-v0.3.6) (2026-11-11)


### Bug Fixes

* **ide:** the IntelliJ plugin updates without a restart: its icon and the axx host no longer keep the old version loaded ([#318](https://github.com/nimbusxr/axx/issues/318)) ([b9005db](https://github.com/nimbusxr/axx/commit/b9005db270ca5355efe4d676277dbf8209577015))

## [0.3.5](https://github.com/nimbusxr/axx/compare/intellij-v0.3.4...intellij-v0.3.5) (2026-11-11)


### Bug Fixes

* **deps:** every pending update, the majors among them, in one ([#316](https://github.com/nimbusxr/axx/issues/316)) ([ef5f2a5](https://github.com/nimbusxr/axx/commit/ef5f2a5d190f4f0f43325c901ed9a3bc920273d1))

## [0.3.4](https://github.com/nimbusxr/axx/compare/intellij-v0.3.3...intellij-v0.3.4) (2026-11-11)


### Features

* **cli:** axx fmt lays out feature files the one way (indentation, aligned tables, blank lines) without changing what they say, and axx fmt --check lists those it would change ([affa79c](https://github.com/nimbusxr/axx/commit/affa79c967a6d5e60741e07614d477620ff4be2a))
* **config:** axx.yaml's fmt.indent, 2 (the default) or 4, sets how far axx fmt and editors indent features ([affa79c](https://github.com/nimbusxr/axx/commit/affa79c967a6d5e60741e07614d477620ff4be2a))
* **init:** the CI workflow axx init writes checks the features' layout with axx fmt --check, and agents run axx fmt on the features they write ([affa79c](https://github.com/nimbusxr/axx/commit/affa79c967a6d5e60741e07614d477620ff4be2a))
* **intellij:** Reformat Code formats feature files through axx, in place of the Gherkin plugin's formatter ([affa79c](https://github.com/nimbusxr/axx/commit/affa79c967a6d5e60741e07614d477620ff4be2a))
* **lsp:** VS Code and other editors format feature files through axx lsp, as axx fmt does ([affa79c](https://github.com/nimbusxr/axx/commit/affa79c967a6d5e60741e07614d477620ff4be2a))

## [0.3.3](https://github.com/nimbusxr/axx/compare/intellij-v0.3.2...intellij-v0.3.3) (2026-11-08)


### Features

* a service's command, env and cleanup are filled in as it starts, with what the services it depends on told; one that started too early stops the run ([79f3d3b](https://github.com/nimbusxr/axx/commit/79f3d3b0a2704112109c9c5314fb853218410d42))
* services and hooks tell axx properties, like the ports Docker picked, in the file AXX_PROPERTIES names; scenarios and readiness checks read them as ${sys:name}, and a value you set (-D, axx.yaml, a profile) wins ([79f3d3b](https://github.com/nimbusxr/axx/commit/79f3d3b0a2704112109c9c5314fb853218410d42))


### Bug Fixes

* **intellij:** the IntelliJ plugin and the VS Code extension complete axx.yaml from the schemas of the axx the project runs on, so a newer axx needs no plugin release ([79f3d3b](https://github.com/nimbusxr/axx/commit/79f3d3b0a2704112109c9c5314fb853218410d42))

## [0.3.2](https://github.com/nimbusxr/axx/compare/intellij-v0.3.1...intellij-v0.3.2) (2026-11-07)


### Features

* axx.yaml's hooks.commands run a project's own commands before or after axx's commands ([28f3657](https://github.com/nimbusxr/axx/commit/28f3657c0071562fde5618dfaf40939cc54afa34))
* **intellij:** the axx host asks in a dialog in front of the IDE, says which run keeps the services, restarts from the status bar, and can keep the Run window in the back ([28f3657](https://github.com/nimbusxr/axx/commit/28f3657c0071562fde5618dfaf40939cc54afa34))
* packs can run hooks before or after axx's commands (core.CommandHook) ([28f3657](https://github.com/nimbusxr/axx/commit/28f3657c0071562fde5618dfaf40939cc54afa34))


### Bug Fixes

* **fixtures:** a dataset's SQL schema is read as the database reads it, from every database's own output and later column changes in six dialects ([28f3657](https://github.com/nimbusxr/axx/commit/28f3657c0071562fde5618dfaf40939cc54afa34))
* **host:** a run that waits for another says which run has the services ([28f3657](https://github.com/nimbusxr/axx/commit/28f3657c0071562fde5618dfaf40939cc54afa34))

## [0.3.1](https://github.com/nimbusxr/axx/compare/intellij-v0.3.0...intellij-v0.3.1) (2026-11-06)


### Bug Fixes

* **intellij:** the plugin bundles axx's schemas from its own folder, so a schema change releases it ([#300](https://github.com/nimbusxr/axx/issues/300)) ([5967009](https://github.com/nimbusxr/axx/commit/5967009e8ea5935c013b6cf084c09c3b5935fdd4))

## [0.3.0](https://github.com/nimbusxr/axx/compare/intellij-v0.2.2...intellij-v0.3.0) (2026-11-04)


### ⚠ BREAKING CHANGES

* `axx lint` now fails on a second When or Then in a scenario, and on a When after a Then. Use And, or split the scenario; `lint: {config: {mode: warn}}` in axx.yaml makes them warnings.

### Features

* a project names the axx version it runs on, axx upgrades itself, and a scenario has one When and one Then ([#295](https://github.com/nimbusxr/axx/issues/295)) ([b84ab6d](https://github.com/nimbusxr/axx/commit/b84ab6d78afc481f98bb87a371076f6b86d5ae66))

## [0.2.2](https://github.com/nimbusxr/axx/compare/intellij-v0.2.1...intellij-v0.2.2) (2026-10-31)


### Features

* axx host starts the services for the runs of an agent in a sandbox without Docker ([#278](https://github.com/nimbusxr/axx/issues/278)) ([9041256](https://github.com/nimbusxr/axx/commit/9041256738824e91dbd97c490189db0e8b3c4d00))

## [0.2.1](https://github.com/nimbusxr/axx/compare/intellij-v0.2.0...intellij-v0.2.1) (2026-10-27)


### Bug Fixes

* the security alerts' fixes and every pending dependency update ([#263](https://github.com/nimbusxr/axx/issues/263)) ([e57a6e0](https://github.com/nimbusxr/axx/commit/e57a6e0d0d78f9e98fc24813d20c361edf62691a))

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
