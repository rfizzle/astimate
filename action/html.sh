#!/usr/bin/env bash
# Write the HTML report for the composite action in action.yml:
# `astimate report --base <base> --format html --out astimate-report.html`,
# which the action then uploads as the astimate-report artifact. It never
# fails the job: the gate's verdict is the check step's.
#
# Environment: ASTIMATE_BASE, ASTIMATE_CONFIG, ASTIMATE_PATH (as run.sh).
set -uo pipefail

args=(report --format html --out astimate-report.html)
if [ -n "${ASTIMATE_CONFIG:-}" ]; then
	args+=(--config "$ASTIMATE_CONFIG")
fi
if [ -n "${ASTIMATE_BASE:-}" ]; then
	args+=(--base "$ASTIMATE_BASE")
fi
if ! astimate "${args[@]}" -- "${ASTIMATE_PATH:-.}"; then
	echo "::warning title=astimate report::the HTML report was not written; see the log above" >&2
fi
exit 0
