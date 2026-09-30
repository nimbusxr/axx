# Changelog

## [0.1.1](https://github.com/nimbusxr/axx/compare/wiremock-openapi-v0.1.0...wiremock-openapi-v0.1.1) (2026-09-30)


### Features

* **a2a:** send messages to an A2A agent over JSON-RPC, HTTP+JSON or gRPC, and check its card, its tasks, their artifacts and its streams ([b44fcfc](https://github.com/nimbusxr/axx/commit/b44fcfc054642bd2ac8c70512fa1dab917ab3826))
* **graphql:** send queries, mutations and subscriptions to GraphQL services, a federated graph's gateway or a subgraph alone, checked against their schemas, with validation levels ([754be6a](https://github.com/nimbusxr/axx/commit/754be6a211c6934f0bb8caf4c566738adeeb84a5))
* **grpc:** call gRPC services, unary and server-streaming, and check their status, answers, metadata and streamed messages, read with their protos or reflection ([4df5051](https://github.com/nimbusxr/axx/commit/4df50518702957fb26ecd837eb875beccef1efe1))
* **jsonrpc:** call JSON-RPC 2.0 services and check their results and errors, against their OpenRPC documents, with validation levels ([4df5051](https://github.com/nimbusxr/axx/commit/4df50518702957fb26ecd837eb875beccef1efe1))
* **mcp:** call an MCP server's tools as an assistant does, over stdio or streamable HTTP, with the tools' schemas as the contract; read its resources and prompts ([b44fcfc](https://github.com/nimbusxr/axx/commit/b44fcfc054642bd2ac8c70512fa1dab917ab3826))
* **mock:** check the tools a service called on a mocked MCP server, and the messages it sent a mocked A2A agent ([b44fcfc](https://github.com/nimbusxr/axx/commit/b44fcfc054642bd2ac8c70512fa1dab917ab3826))
* **mock:** check what a service asked a mocked model: the texts it was asked about, what a request sent and never sent, the tools offered and the schema asked for ([235dd7b](https://github.com/nimbusxr/axx/commit/235dd7b128098627dd13d6b51da407eb096eb0d1))
* **wiremock:** the axx-wiremock image mocks AI models in the format of each request (OpenAI's API and the servers that speak it, Anthropic, Gemini, Bedrock, Ollama), with streams, provider-shaped failures and deterministic embeddings ([235dd7b](https://github.com/nimbusxr/axx/commit/235dd7b128098627dd13d6b51da407eb096eb0d1))
* **wiremock:** the axx-wiremock image mocks an MCP server from its description and an A2A agent from its card ([b44fcfc](https://github.com/nimbusxr/axx/commit/b44fcfc054642bd2ac8c70512fa1dab917ab3826))
* **wiremock:** the axx-wiremock image mocks GraphQL services and federated subgraphs field by field from their schema, with _service and _entities ([754be6a](https://github.com/nimbusxr/axx/commit/754be6a211c6934f0bb8caf4c566738adeeb84a5))
* **wiremock:** the axx-wiremock image mocks gRPC services with WireMock's gRPC extension, when its root has a grpc folder ([4df5051](https://github.com/nimbusxr/axx/commit/4df50518702957fb26ecd837eb875beccef1efe1))


### Bug Fixes

* **cli:** on Windows, what a command left running has ended before its scenario's folder is removed ([b44fcfc](https://github.com/nimbusxr/axx/commit/b44fcfc054642bd2ac8c70512fa1dab917ab3826))
* **graphql,jsonrpc,grpc:** values built from a table keep a string's leading zeros, and build nested paths ([b44fcfc](https://github.com/nimbusxr/axx/commit/b44fcfc054642bd2ac8c70512fa1dab917ab3826))
* **wiremock:** the axx-wiremock image downloads WireMock and its gRPC extension until their sha256 matches, instead of failing on one download that went wrong ([72a1e4a](https://github.com/nimbusxr/axx/commit/72a1e4a63f9d4e1e1b41895560a768809f5ddfda))

## 0.1.0 (2026-09-25)


### Features

* init commit ([63bf642](https://github.com/nimbusxr/axx/commit/63bf642d25367fbb7c5a0d19c3f877b54fae4ba1))
