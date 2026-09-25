# Answers that must not earn a reward

Each directory is a weak or cheating answer to one task, copied over the starting project with
`go run ./cmd/evals check --patch`. Every one must get reward 0; together they exercise the
verifier's rules end to end (the unit tests in `internal/verify` cover each rule on its own).

| Directory | Task | Why it fails |
| --- | --- | --- |
| `weak-assertions` | `rest-crud-happy-path` | Only checks status codes of the registration: the update and cancel mutants survive. |
| `captured-variable` | `sql-manifest-import` | Passes a value between steps with `${var:...}`: `variables`. |

```sh
go run ./cmd/evals check --patch testdata/negative/weak-assertions rest-crud-happy-path
go run ./cmd/evals check --patch testdata/negative/captured-variable sql-manifest-import
```
