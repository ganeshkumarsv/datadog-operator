#!/usr/bin/env bash

set -exo pipefail

export LC_ALL=C

ROOT=$(git rev-parse --show-toplevel)

$ROOT/bin/wwhrd list

echo Component,Origin,License > "$ROOT/LICENSE-3rdparty.csv"
echo 'core,"github.com/frapposelli/wwhrd",MIT' >> "$ROOT/LICENSE-3rdparty.csv"
unset grep
$ROOT/bin/wwhrd list --no-color |& grep "Found License" | awk '{print $6,$5}' | sed -E "s/\x1B\[([0-9]{1,2}(;[0-9]{1,2})?)?[mGK]//g" | sed s/" license="/,/ | sed s/package=/core,/ | sort >> "$ROOT/LICENSE-3rdparty.csv"
