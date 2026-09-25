#!/usr/bin/env bash
# Reference solution: the feature files a good answer adds.
#
# Written against the kafka pack's step text (testdata/steps.json); the task
# runs once the axx build under test has the kafka pack (task.toml: requires).
set -euo pipefail
cp -R /solution/files/. /app/
