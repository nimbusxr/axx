#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Starts a sandbox IntelliJ IDEA to try the axx plugin: the plugin built from ide/intellij, the
# Go plugin, and axx built from this tree (bin/axx-all, every pack in it), on a project.
#   usage: scripts/intellij-sandbox.sh [project]   (default: examples/parcels/acceptance)
# The Go plugin needs an IntelliJ IDEA subscription: sign in once in the sandbox. Settings made
# in the sandbox are kept, and the Go plugin is always there; another plugin installed in it
# restarts the IDE, which closes it: start it again.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
project=$(cd "${1:-$root/examples/parcels/acceptance}" && pwd)

echo "building axx with every pack: bin/axx-all"
(cd "$root" && go build -o bin/axx-all ./internal/tools/axxall)

cd "$root/ide/intellij"
./gradlew --no-daemon --console=plain --quiet prepareSandbox_runIdeWithGo

# The sandbox's settings (runIdeWithGo shares runIde's sandbox): the plugin runs bin/axx-all,
# and the project is trusted. Other settings made in the sandbox are kept.
version=$(sed -n 's/^platformVersion=//p' gradle.properties)
options=".intellijPlatform/sandbox/axx-intellij/IU-$version/config_runIde/options"
mkdir -p "$options"
export EXECUTABLE="$root/bin/axx-all" PROJECT="$project"
if [[ ! -f "$options/axx.xml" ]]; then
  cat >"$options/axx.xml" <<XML
<application>
  <component name="us.nimbusxr.axx.idea.AxxSettings">
    <option name="executable" value="$EXECUTABLE" />
  </component>
</application>
XML
elif grep -q '<option name="executable"' "$options/axx.xml"; then
  perl -pi -e 's{<option name="executable" value="[^"]*" />}{<option name="executable" value="$ENV{EXECUTABLE}" />}' "$options/axx.xml"
else
  perl -pi -e 's{(<component name="us.nimbusxr.axx.idea.AxxSettings">)}{$1\n    <option name="executable" value="$ENV{EXECUTABLE}" />}' "$options/axx.xml"
fi
if [[ ! -f "$options/trusted-paths.xml" ]]; then
  cat >"$options/trusted-paths.xml" <<XML
<application>
  <component name="Trusted.Paths.Settings">
    <option name="TRUSTED_PATHS">
      <list>
        <option value="$PROJECT" />
      </list>
    </option>
  </component>
</application>
XML
elif ! grep -qF "<option value=\"$PROJECT\" />" "$options/trusted-paths.xml"; then
  perl -pi -e 's{(<list>)}{$1\n        <option value="$ENV{PROJECT}" />}' "$options/trusted-paths.xml"
fi

# Without a licence, IntelliJ IDEA runs without its Ultimate features, and the Go plugin, which
# needs them, is not loaded.
if [[ ! -f "$options/../idea.key" ]]; then
  echo "note: the sandbox has no licence yet, so the Go plugin is not loaded: activate a subscription"
  echo "      or a trial in it (Help | Register...), then start it again"
fi

echo "starting IntelliJ IDEA on $project"
exec ./gradlew --no-daemon --console=plain runIdeWithGo --args="$project"
