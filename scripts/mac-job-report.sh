#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# What a Mac job's log does not say of a failure, printed at the job's end (the simulator-mac
# action's report):
#
# The crash, hang and resource reports macOS wrote during the job, a line each, oldest first:
# when, which process, what ended it. A simulator's processes are the Mac's, so their reports are
# among the Mac's (marked: the simulator's launchd started them; CoreSimulator's own services,
# which keep the simulators, are the Mac's). A simulator that stops being booted in
# the middle of the tests, or a WebDriverAgent that never answers, leaves the report of the
# process that crashed or was ended here, and the job's log showed only that the crash reporter
# had been busy. With a folder, the reports are copied there, for the job to keep.
#
# And what the runner's own log says of cancelling the job: GitHub's runner cancels a job's step
# when the service tells it to (a run cancelled, a newer run of the same branch) and when it can
# no longer renew its hold on the job, and the job's log says "The operation was canceled." for
# both.
#   usage: scripts/mac-job-report.sh <a file written as the job began> [a folder to copy the reports to]
# For the script's own test: MAC_JOB_REPORTS, the folders of reports (separated by colons), and
# MAC_JOB_RUNNER_LOGS, the folder of the runner's logs.
set -uo pipefail

began=${1:?a file written as the job began}
keep=${2:-}

IFS=: read -r -a folders <<< "${MAC_JOB_REPORTS:-$HOME/Library/Logs/DiagnosticReports:/Library/Logs/DiagnosticReports}"

# A report in Apple's .ips format is a line of JSON (the report's name, kind and time) and then a
# JSON document: of a crash, the process, its parent, the exception and what ended it. A report
# without a name (a kind that is of no one process) is told by its file's name.
ips() {
  local file=$1 head body
  head=$(head -1 "$file" | jq -r --arg file "$(basename "$file")" '
    [.timestamp // "", ([.app_name, .name, $file] | map(select(. != null and . != "")) | first), "kind " + (.bug_type // "?" | tostring)] | join("  ")' 2> /dev/null) || head=
  body=$(tail -n +2 "$file" | jq -r '
    [ (if .parentProc == "launchd_sim" then "(simulator)" else empty end),
      (if .parentProc then "started by " + .parentProc else empty end),
      (if .exception then ([.exception.type, .exception.signal] | map(select(. != null)) | join(" ")) else empty end),
      (if .termination then "ended by " + (.termination.byProc // "?") + ": " +
        ([.termination.namespace, .termination.indicator] | map(select(. != null) | tostring) | join(" ")) else empty end)
    ] | join("; ")' 2> /dev/null) || body=
  echo "  ${head:-$(basename "$file")}${body:+  $body}"
}

# Any other report is text: a spindump of a process that hung (.spin, .hang), or a process that
# used too much of the processor or woke too often (.diag). Its first lines name the process and
# say why it was written.
text() {
  local file=$1
  echo "  $(basename "$file")  $(grep -m 4 -E '^(Command|Process|Event|Reason|Action taken|Duration):' "$file" 2> /dev/null | tr -s ' \t' ' ' | paste -sd ';' -)"
}

echo "the reports macOS wrote during the job:"
found=0
while IFS= read -r file; do
  [[ -n "$file" ]] || continue
  found=$((found + 1))
  case "$file" in
    *.ips) ips "$file" ;;
    *) text "$file" ;;
  esac
  if [[ -n "$keep" ]]; then
    mkdir -p "$keep" && cp "$file" "$keep/" 2> /dev/null
  fi
done < <(
  for folder in "${folders[@]}"; do
    find "$folder" -maxdepth 1 -type f -newer "$began" \( -name '*.ips' -o -name '*.spin' -o -name '*.hang' -o -name '*.diag' -o -name '*.crash' \) 2> /dev/null
  done | while IFS= read -r file; do echo "$(stat -f %m "$file" 2> /dev/null || stat -c %Y "$file") $file"; done | sort -n | cut -d' ' -f2-
)
((found > 0)) || echo "  none"

# The runner's logs are in the _diag folder beside the folder of its programs. Its listener is
# found by its program's path alone: its arguments are not read, and not printed.
logs=${MAC_JOB_RUNNER_LOGS:-}
if [[ -z "$logs" ]]; then
  listener=$(pgrep -x Runner.Listener | head -1 || true)
  [[ -z "$listener" ]] || logs=$(dirname "$(dirname "$(ps -o comm= -p "$listener")")")/_diag
fi
echo "what the runner's log says of cancelling the job:"
if [[ -z "$logs" || ! -d "$logs" ]]; then
  echo "  the runner's logs were not found${logs:+ in $logs}"
else
  # The listener's log: it takes the service's messages and renews the job, and tells the
  # worker to cancel. Its renewals that went well, one a minute, are left out.
  said=$(cat "$logs"/Runner_*.log 2> /dev/null |
    grep -iE 'cancel|abandon|renew|lost (the )?connection' | grep -v 'Successfully renew' | tail -40 | cut -c1-400)
  if [[ -n "$said" ]]; then echo "  ${said//$'\n'/$'\n'  }"; else echo "  nothing"; fi
fi
exit 0
