---
title: Test AI features
description: "Mock the models your service asks with the Axx WireMock image, in the format of whichever provider's API it speaks, and check what your service asked them: the prompt, the tools, the schema, and what it must never send."
---

A service that asks a model is tested like a service that calls any other dependency. The model is a mock, its answers are stubs, and the scenarios check what the service does with each answer and what it asked. A live model answers differently from run to run, and it will not produce the answers that break AI features on demand: a malformed answer, a rate limit, a stream that stops halfway.

The Axx WireMock image's model mock answers in the format of the request, whichever provider's API your service speaks:

```gherkin
Scenario: The assistant looks the parcel up before it answers, and never tells the model the street
  Given a seeds/assistant-parcel.yaml db seed
  And a POST request to /api/assistant/questions
  And a request payload using an application/json content example
  And the request payload property question is 'Where is my parcel PX-AI-8102?'
  When the request is executed
  Then the response status code is 200
  And the response payload property answer is 'Your parcel PX-AI-8102 is out for delivery in Leipzig and arrives today.'
  And the mocked models model was offered the track_parcel tool in the request about 'PX-AI-8102'
  And the mocked models model's request about 'PX-AI-8102' contains 'OUT_FOR_DELIVERY'
  And the mocked models model's request about 'PX-AI-8102' does not contain 'Lindenweg 14'
```

## Point your service at the mock

Run the image as the model, and give your service its URL as the model's base URL: every SDK takes one.

```yaml title="infra/compose.yaml (excerpt)"
models:
  image: ghcr.io/nimbusxr/axx-wiremock:<version>   # a version with model mocks
  ports:
    - '8086:8080'
  volumes:
    - './models:/home/wiremock'
app:
  environment:
    PARCELS_MODEL_URL: http://models:8080/v1
```

Nothing says which provider the mock is: it answers each request in its own API's format.

| Your service speaks | Its base URL | The mock answers |
| --- | --- | --- |
| OpenAI's API, and the servers that speak it (Azure OpenAI, vLLM, llama.cpp, LM Studio, Ollama's `/v1`, Mistral, DeepSeek, OpenRouter, LiteLLM...) | `http://models:8080/v1` | Chat Completions, Responses, embeddings, models |
| Anthropic's API, and the servers that speak it | `http://models:8080` | Messages and models; Claude on Bedrock (`invoke`) and on Vertex AI (`rawPredict`) |
| Gemini, on Google AI Studio or Vertex AI | `http://models:8080` | `generateContent`, `embedContent`, `batchEmbedContents`, models |
| Amazon Bedrock | `http://models:8080` (the SDK's endpoint override) | Converse and ConverseStream; Titan and Cohere embeddings (`invoke`) |
| Ollama's own API | `http://models:8080` | `/api/chat`, `/api/generate`, `/api/embed`, `/api/embeddings`, `/api/tags` |

The answers are checked against the providers' own contracts: their official SDKs (OpenAI's, Anthropic's, Google's and AWS's) read them in the image's tests, and OpenAI's and Ollama's published OpenAPI documents validate them.

## Write the answers

An answer is a WireMock mapping file. The `model-request` matcher says which requests the stub answers, and the `model-answer` transformer renders the stub's answer, written the same way for every provider:

```json title="infra/models/mappings/address-note.json"
{
  "name": "The address in Mara Lindqvist's note",
  "request": {
    "customMatcher": {"name": "model-request", "parameters": {"about": "Birkenallee 3"}}
  },
  "response": {
    "transformers": ["model-answer"],
    "jsonBody": {
      "json": {"name": "Mara Lindqvist", "street": "Birkenallee 3", "postcode": "01067", "city": "Dresden", "country": "DE"}
    }
  }
}
```

### Which requests a stub answers

The `model-request` matcher takes:

| Parameter | Matches |
| --- | --- |
| `about` | A text, or a list of texts, that the conversation has: in a message, the system prompt, or a tool's call or result. JSON-escaped text counts (`Hauptstraße` has `Hauptstraße`). For embeddings, the texts to embed. |
| `afterTool` | The tool whose result the model was given last, since the user last wrote. A stub without it answers only when the model has had no tool result since. |
| `endpoint` | `chat` (the default), `embeddings`, or `models`. |

- **Keep scenarios apart** with `about`: name data that only one scenario has, like its parcel's reference or its recipient's street.
- **When stubs overlap,** WireMock's `priority` decides, as for any stub.
- **A mistake in a stub stops WireMock** at startup, and names the stub: an unknown key, or a wrong value.

### What the model answers

| Key | The answer |
| --- | --- |
| `text` | Its text. |
| `json` | Its structured output: a JSON value, which the answer sends as its text. |
| `toolCalls` | The tools it calls: a list of `name`, `arguments` (an object), and optionally `id`. |
| `reasoning` | Its reasoning, as the provider shows it: OpenAI Responses' reasoning summary, Anthropic's thinking, Gemini's thoughts, Bedrock's reasoning content, Ollama's thinking. For the servers that speak OpenAI's Chat Completions API, `reasoning_content` and `reasoning`. |
| `refusal` | A refusal, in the provider's refusal field where it has one (OpenAI's `refusal`, Anthropic's `refusal` stop reason), and as its text elsewhere. |
| `stop` | Why it stopped early: `length` (at its token limit) or `safety` (blocked). |
| `usage` | The tokens it reports: `inputTokens`, `outputTokens`, `reasoningTokens`. Estimated from the text when not given. |
| `embedding`, `dimensions` | For embeddings: the vector every text gets, or the dimensions of the vectors. |
| `models` | For models: the names of the models the provider lists. |

A tool loop is a stub per turn: the model calls the tool, then answers once it has the tool's result. A mapping file can hold both:

```json title="infra/models/mappings/where-is-px-ai-8102.json"
{
  "mappings": [
    {
      "request": {"customMatcher": {"name": "model-request", "parameters": {"about": "PX-AI-8102"}}},
      "response": {
        "transformers": ["model-answer"],
        "jsonBody": {"toolCalls": [{"name": "track_parcel", "arguments": {"reference": "PX-AI-8102"}}]}
      }
    },
    {
      "request": {"customMatcher": {"name": "model-request", "parameters": {"about": "PX-AI-8102", "afterTool": "track_parcel"}}},
      "response": {
        "transformers": ["model-answer"],
        "jsonBody": {"text": "Your parcel PX-AI-8102 is out for delivery in Leipzig and arrives today."}
      }
    }
  ]
}
```

- **Embeddings are deterministic:** each text gets a unit vector made from the SHA-256 of the text, so the same text always gets the same vector. It has as many dimensions as the request asks for, or else the stub's `dimensions`, or else the provider's usual number.
- **The mock remembers its Responses:** a request to OpenAI's Responses API that continues an earlier response (`previous_response_id`) is read with that response's conversation.

### Streams

The mock streams whenever the request asks for a stream, in the provider's own encoding:

| API | Its stream |
| --- | --- |
| OpenAI Chat Completions (`"stream": true`) | Server-sent chunks, ending with `data: [DONE]`. With `stream_options.include_usage`, the usage comes in a chunk of its own, before it. |
| OpenAI Responses (`"stream": true`) | Typed server-sent events, from `response.created` to `response.completed`. |
| Anthropic (`"stream": true`) | Server-sent events, from `message_start` to `message_stop`, with a `ping`. On Bedrock, in AWS's event stream. |
| Gemini (`streamGenerateContent`) | Server-sent events with `alt=sse`, a JSON array without. |
| Bedrock (`converse-stream`) | AWS's binary event stream, each frame with its CRCs. |
| Ollama (unless `"stream": false`) | Newline-delimited JSON, ending with `"done": true`. |

### Failures

| Key | The failure |
| --- | --- |
| `error` | The provider's error, in its own shape and status: `type` is `rate_limit`, `overloaded`, `context_length`, `auth` or `server`. `message` replaces the provider's usual message, and `retryAfter` is the seconds to wait (a `Retry-After` header, or Gemini's `RetryInfo`; Bedrock, like AWS, sends none). With `afterEvents`, a stream sends that many events first, then the error, as the provider sends it in a stream. |
| `cutOffAfter` | A stream ends after that many events, without its end. |
| `malformed` | The answer breaks off in the middle of its JSON. |

| `type` | OpenAI | Anthropic | Gemini | Bedrock | Ollama |
| --- | --- | --- | --- | --- | --- |
| `rate_limit` | 429 `rate_limit_exceeded` | 429 `rate_limit_error` | 429 `RESOURCE_EXHAUSTED` | 429 `ThrottlingException` | 429 |
| `overloaded` | 503 `server_error` | 529 `overloaded_error` | 503 `UNAVAILABLE` | 503 `ServiceUnavailableException` | 503 |
| `context_length` | 400 `context_length_exceeded` | 400 `invalid_request_error` | 400 `INVALID_ARGUMENT` | 400 `ValidationException` | 400 |
| `auth` | 401 `invalid_api_key` | 401 `authentication_error` | 400 `API_KEY_INVALID` (401 `UNAUTHENTICATED` on Vertex AI) | 403 `UnrecognizedClientException` | 401 |
| `server` | 500 `server_error` | 500 `api_error` | 500 `INTERNAL` | 500 `InternalServerException` | 500 |

A slow model is WireMock's own: `fixedDelayMilliseconds` delays the answer, and `chunkedDribbleDelay` sends it slowly, in chunks.

```json title="infra/models/mappings/busy.json"
{
  "request": {"customMatcher": {"name": "model-request", "parameters": {"about": "PX-AI-8104"}}},
  "response": {
    "transformers": ["model-answer"],
    "jsonBody": {"error": {"type": "rate_limit", "retryAfter": 1}}
  }
}
```

## Check what your service asked

Register the mock as any mocked service, then check the requests the model got. A request is about a text when its conversation has it, as the `about` of a stub reads it.

```gherkin
Given the mocked models service with the following properties:
  | url | http://${sys:local.host}:8086 |
Then the mocked models model was asked about 'PX-AI-8104' 2 times
And the mocked models model was offered the track_parcel tool in the request about 'PX-AI-8102'
And the mocked models model's request about 'PX-AI-8102' contains 'OUT_FOR_DELIVERY'
And the mocked models model's request about 'PX-AI-8102' does not contain 'Lindenweg 14'
And the mocked models model was asked for the schemas/recipient-address.json schema in the request about 'Birkenallee 3'
```

- **`contains` and `does not contain`** read everything the request sent: its messages, its tools' results, its tools and its settings, JSON-escaped text included. A service that must never send a recipient's street to a model proves it with `does not contain`; the check fails when the model was never asked about the text at all.
- **The schema** is a JSON Schema file of the project (JSON or YAML), compared with the structured output the request asks for, whatever the order of its keys.
- **Every request since the run started counts,** other scenarios' too: name the scenario's own data.

The mock pack's other steps still reach any request by its path and payload. See the [mock pack's reference](/references/packs/mock/) for every step.

## Record real answers

To see what a real model answers, run the image as a recording proxy in front of the provider, once, and point your service at it with a real key:

```sh
docker run --rm -p 8086:8080 -v "$PWD/recordings:/home/wiremock" \
  ghcr.io/nimbusxr/axx-wiremock:<version> \
  --proxy-all=https://api.openai.com --record-mappings
```

WireMock writes a mapping file of each exchange in `recordings/mappings`, in the provider's own format, and the answers' bodies in `recordings/__files`. A recorded mapping answers only the very request it recorded, as it was answered; review it, and never commit a key. To keep the scenarios readable, turn what the model answered into `model-answer` stubs: its text, its tool calls, its structured output.

Recordings test your service against answers a model once gave. Whether a model still answers your prompts well is a different question, for evaluations run against the model itself.
