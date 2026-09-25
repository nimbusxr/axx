`features/manifest-rejections.feature` was written last sprint for the manifest import, but it
has never passed. The import itself works as documented in `docs/manifest-import.md`: the
documentation and the service are right, the feature is wrong.

Please make the feature pass (see `README.md` for the service and how the tests run) without
weakening it: keep both scenarios and everything they check, so the feature still fails if
the import stops behaving as documented. Change only `features/` and `seeds/`.
