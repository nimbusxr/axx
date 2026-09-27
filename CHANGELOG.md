# Changelog

## [0.1.2](https://github.com/nimbusxr/axx/compare/v0.1.1...v0.1.2) (2026-09-27)


### Features

* failed cleanups are kept, REST ordinals are linted, payloads come from files, mocks check bodies ([#23](https://github.com/nimbusxr/axx/issues/23)) ([e028593](https://github.com/nimbusxr/axx/commit/e0285934aa0b51fb3c2bc4c212c5b2625d8003bc))

## [0.1.1](https://github.com/nimbusxr/axx/compare/v0.1.0...v0.1.1) (2026-09-27)


### Features

* axx init connects the agents a repository uses, with a test-data skill ([#18](https://github.com/nimbusxr/axx/issues/18)) ([0508986](https://github.com/nimbusxr/axx/commit/0508986f05be50ae75dff03ce7f979b76a5222ae))
* factory JSON Schemas can reference local schema files ([#12](https://github.com/nimbusxr/axx/issues/12)) ([13b52c7](https://github.com/nimbusxr/axx/commit/13b52c7f60ee4f7e30785f1ee1a972b40877f886))
* **fixtures:** identity values are unique per field name, or per namespace ([#20](https://github.com/nimbusxr/axx/issues/20)) ([67617d3](https://github.com/nimbusxr/axx/commit/67617d39820137c47d2662a54b4e3a38e3e4ac53))
* web and files packs, with Playwright's tools speaking steps ([#3](https://github.com/nimbusxr/axx/issues/3)) ([89c3bb7](https://github.com/nimbusxr/axx/commit/89c3bb7c7cd50ee7badca796ac24261233130f56))


### Bug Fixes

* a fixture reference sees the document its fixture generates ([#11](https://github.com/nimbusxr/axx/issues/11)) ([9107b97](https://github.com/nimbusxr/axx/commit/9107b971319c9098742455caa0e0b96b59c318f3))
* a project's build of axx says which axx and packs it holds ([#15](https://github.com/nimbusxr/axx/issues/15)) ([7880959](https://github.com/nimbusxr/axx/commit/7880959fc506b24c34b0b3265dada27a23b97c43))
* a REST request's Header() changes the request it belongs to ([#16](https://github.com/nimbusxr/axx/issues/16)) ([fe54d43](https://github.com/nimbusxr/axx/commit/fe54d4354e4f869dd4aa63e2390e6f239bfeb64a))
* adoption shares only what every file has, and declares the identities chosen ([#17](https://github.com/nimbusxr/axx/issues/17)) ([d7de380](https://github.com/nimbusxr/axx/commit/d7de380ca3b4afd1fd1dd7674f9467db06cae021))
* axx up works in a project with packs ([#10](https://github.com/nimbusxr/axx/issues/10)) ([a6081f0](https://github.com/nimbusxr/axx/commit/a6081f02bb3ea1e9c3c05ccc20ccce4bfb0ba788))
* fixtures check passes a fresh clone and catches a stale manifest ([#13](https://github.com/nimbusxr/axx/issues/13)) ([012f839](https://github.com/nimbusxr/axx/commit/012f839963afe392bacf065665e3a4e47f451e8f))
* **gcp-firestore:** a seed's unquoted YAML dates are timestamps ([#14](https://github.com/nimbusxr/axx/issues/14)) ([b970c0e](https://github.com/nimbusxr/axx/commit/b970c0edfe33b0d77a7582579ba8cc8b657841b8))
* **skills:** the test-data skill says how identity values are compared ([#22](https://github.com/nimbusxr/axx/issues/22)) ([9182dba](https://github.com/nimbusxr/axx/commit/9182dba1c63ca33d2d34448b5e94a81e065b5bac))
* web-coverage takes a step's counts before the next step can close a tab ([#6](https://github.com/nimbusxr/axx/issues/6)) ([7b64a95](https://github.com/nimbusxr/axx/commit/7b64a95ce7c144262e1d94d89c16a2a4c43c8a42))

## 0.1.0 (2026-09-25)


### Features

* init commit ([63bf642](https://github.com/nimbusxr/axx/commit/63bf642d25367fbb7c5a0d19c3f877b54fae4ba1))
