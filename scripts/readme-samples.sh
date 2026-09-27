#!/usr/bin/env bash
# readme-samples.sh regenerates the command samples in README.md from the
# fixture modules under testdata/, so the README shows what the CLI prints
# rather than hand-typed output.
#
#   scripts/readme-samples.sh           rewrite README.md in place
#   scripts/readme-samples.sh --check   regenerate into a temporary copy and
#                                       fail with a diff when README.md drifts
#
# Each sample sits between <!-- sample:<name> --> and <!-- /sample:<name> -->
# and is replaced whole. A marker without a sample, or a sample without a
# marker, is an error. Output that depends on the build (the version, commit
# and date) and temporary paths are normalized so the check is stable.
set -euo pipefail

cd "$(dirname "$0")/.."

mode=write
case "${1:-}" in
"") ;;
--check) mode=check ;;
*)
	echo "usage: scripts/readme-samples.sh [--check]" >&2
	exit 1
	;;
esac

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
samples="$tmp/samples"
mkdir "$samples"

bin="$tmp/astimate"
go build -o "$bin" ./cmd/astimate

# normalize replaces build-dependent values and the temporary directory.
normalize() {
	sed -e "s|$tmp|/tmp|g" \
		-e 's|^version: .*|version: <version>|' \
		-e 's|^commit: .*|commit: <commit>|' \
		-e 's|^date: .*|date: <date>|' \
		-e 's|"astimate_version": "[^"]*"|"astimate_version": "<version>"|'
}

# sample <name> <shown command> <limit> -- <argv...> runs argv from the
# repository root and writes a fenced block to $samples/<name>: the shown
# command, the first <limit> lines of stdout (0 for all) and, when it is not
# 0, the exit status. Stderr is not captured; the samples write none.
sample() {
	local name="$1" shown="$2" limit="$3"
	shift 4
	local out="$tmp/$name.out" status=0
	"$@" >"$out" || status=$?
	{
		echo '```'
		echo "\$ $shown"
		if [ "$limit" -gt 0 ]; then
			head -n "$limit" "$out"
			echo "..."
		else
			cat "$out"
		fi
		if [ "$status" -ne 0 ]; then
			echo "\$ echo \$?"
			echo "$status"
		fi
		echo '```'
	} | normalize >"$samples/$name"
}

sample assess "astimate assess testdata/go/fixture/dupes" 0 -- \
	"$bin" assess testdata/go/fixture/dupes
sample assess-json "astimate assess testdata/go/fixture/dupes --json | head -25" 25 -- \
	"$bin" assess testdata/go/fixture/dupes --json
sample rank "astimate rank testdata/go/fixture" 0 -- \
	"$bin" rank testdata/go/fixture
sample rank-ts "astimate rank testdata/ts/fixture" 0 -- \
	"$bin" rank testdata/ts/fixture
sample baseline-write "astimate baseline write testdata/go/fixture --out /tmp/fixture-baseline.json" 0 -- \
	"$bin" baseline write testdata/go/fixture --out "$tmp/fixture-baseline.json"
sample check "astimate check testdata/go/fixture-degraded --baseline /tmp/fixture-baseline.json --all" 0 -- \
	"$bin" check testdata/go/fixture-degraded --baseline "$tmp/fixture-baseline.json" --all
mkdir "$tmp/init"
sample config-init "astimate config init" 0 -- \
	sh -c 'cd "$1" && "$2" config init' sh "$tmp/init" "$bin"
sample config-init-head "head -15 astimate.yaml" 15 -- \
	cat "$tmp/init/astimate.yaml"
sample version "astimate version" 0 -- \
	"$bin" version

# config init and the head of the file it wrote read as one session: drop
# the first block's closing fence and the second block's opening one.
{
	sed '$d' "$samples/config-init"
	sed '1d' "$samples/config-init-head"
} >"$tmp/config-init.joined"
mv "$tmp/config-init.joined" "$samples/config-init"
rm "$samples/config-init-head"

out="$tmp/README.md"
awk -v dir="$samples" '
	function fail(msg) {
		print "readme-samples: " msg > "/dev/stderr"
		failed = 1
		exit 1
	}
	/^<!-- sample:[a-z0-9-]+ -->$/ {
		name = substr($0, 13, length($0) - 16)
		file = dir "/" name
		if ((getline line < file) <= 0) fail("README.md has a marker for " name " but the script makes no such sample")
		print
		print line
		while ((getline line < file) > 0) print line
		close(file)
		used[name] = 1
		skipping = 1
		next
	}
	skipping && $0 == "<!-- /sample:" name " -->" { skipping = 0; print; next }
	skipping { next }
	{ print }
	END {
		if (failed) exit 1
		if (skipping) fail("no closing marker for sample " name)
		while ((("ls " dir) | getline f) > 0) {
			if (!(f in used)) fail("sample " f " has no marker in README.md")
		}
	}
' README.md >"$out"

if [ "$mode" = check ]; then
	if ! diff -u README.md "$out"; then
		echo "README.md samples are out of date; run make readme-samples" >&2
		exit 1
	fi
	exit 0
fi
cp "$out" README.md
