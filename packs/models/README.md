# Model stubs

The user guide is [Stub embedded models](../../docs/src/content/docs/guides/stub-embedded-models.md)
(on the site: `/guides/stub-embedded-models/`), including the platform APIs
in [Models the platform provides](../../docs/src/content/docs/guides/stub-embedded-models.md#models-the-platform-provides).

- `core` (`models-core`): contracts, mappings, matching, every format's stubs,
  and `axx models contract|build`.
- `ios`, `android`, `web` (`models-ios`, `models-android`, `models-web`):
  answer platform model APIs in test runs from `.axxmodel` stubs in the app's
  `axx-models` folder. Each pack's `apis.go` lists its APIs and their
  contracts.
