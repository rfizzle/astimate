#!/usr/bin/env bash
# Run `astimate check` for the composite action in action.yml and exit with
# its status: 0 passed, 3 the gate failed, 2 analysis failed. Stdout is left
# alone so the ::error and ::warning workflow commands become annotations.
#
# Environment: ASTIMATE_BASE (empty uses the tool's default ref),
# ASTIMATE_CONFIG (optional path), ASTIMATE_ALL ("true" or "false"),
# ASTIMATE_FORMAT (default "github"), ASTIMATE_PATH (the module root to
# check, default "."; astimate keeps annotation paths relative to the
# repository root).
set -uo pipefail

args=(check --format "${ASTIMATE_FORMAT:-github}")
case "${ASTIMATE_ALL:-false}" in
true) args+=(--all) ;;
false) ;;
*)
	echo "::error title=astimate::input all must be true or false, got ${ASTIMATE_ALL}" >&2
	exit 2
	;;
esac
if [ -n "${ASTIMATE_CONFIG:-}" ]; then
	args+=(--config "$ASTIMATE_CONFIG")
fi
path="${ASTIMATE_PATH:-.}"
if [ ! -d "$path" ]; then
	echo "::error title=astimate::input path $path is not a directory" >&2
	exit 2
fi

base="${ASTIMATE_BASE:-}"
if [ -n "$base" ]; then
	if [ "$(git -C "$path" rev-parse --is-shallow-repository 2>/dev/null)" = true ]; then
		echo "::warning title=astimate::shallow clone; set fetch-depth: 0 on actions/checkout so the merge-base with $base exists" >&2
	fi
	if ! git -C "$path" rev-parse --verify --quiet "$base^{commit}" >/dev/null 2>&1; then
		echo "::error title=astimate::base ref $base not found; check out with fetch-depth: 0 or fetch it first" >&2
		exit 2
	fi
	args+=(--base "$base")
fi

# -- ends the flags, so a path starting with - is still the module root.
astimate "${args[@]}" -- "$path"
status=$?
case "$status" in
0) ;;
3) echo "astimate: the gate failed (exit 3); see the annotations" >&2 ;;
2) echo "astimate: analysis failed (exit 2); see the log above" >&2 ;;
*) echo "astimate: exited $status" >&2 ;;
esac
exit "$status"
