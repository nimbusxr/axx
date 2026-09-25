# Benchmarks

`bench/run.sh` measures axx with [hyperfine](https://github.com/sharkdp/hyperfine)
(installed by `mise install`). Results are written to `bench/results/`, which is
not committed; published numbers go in the release notes.

| Mode | What it measures |
|---|---|
| `startup` | CLI start-up: `axx version`, `axx steps --json`, `axx validate` on the example |
| `suite` | the example suite against an already running stack, with its databases emptied before each run: the local dev loop |
| `e2e` | one cold run: start the stack, run every feature, tear everything down |
