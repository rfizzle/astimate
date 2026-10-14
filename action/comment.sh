#!/usr/bin/env bash
# Post, or update, the one astimate comment on a pull request for the
# composite action in action.yml. The comment is the Markdown of
# `astimate check --format md`, led by a hidden marker; an earlier comment
# with the marker is updated in place, so the pull request never has two.
# It talks to the GitHub REST API with curl and GITHUB_TOKEN, which needs
# `pull-requests: write`. A problem here is a ::warning, never a failure:
# the gate's verdict is the check step's.
#
# Environment: ASTIMATE_BASE, ASTIMATE_CONFIG, ASTIMATE_ALL, ASTIMATE_PATH
# (as run.sh), ASTIMATE_HTML ("true" adds a pointer to the report artifact),
# GITHUB_TOKEN, GITHUB_REPOSITORY, ASTIMATE_PR_NUMBER, GITHUB_API_URL
# (default https://api.github.com).
#
# With ASTIMATE_COMMENT_DRY_RUN=1 nothing is sent: the script prints the
# request it would make, "METHOD URL" and then the JSON body, and finds the
# earlier comment in the listing in the file ASTIMATE_COMMENTS_FILE (the
# API's response to listing the pull request's comments) instead of asking.
set -uo pipefail

marker='<!-- astimate-report -->'

warn() { echo "::warning title=astimate comment::$*" >&2; }

# json_body prints the request body: the comment text $1 as a JSON string.
json_body() {
	local s=$1
	s=${s//\\/\\\\}
	s=${s//\"/\\\"}
	s=${s//$'\n'/\\n}
	s=${s//$'\t'/\\t}
	s=${s//$'\r'/\\r}
	printf '{"body":"%s"}\n' "$s"
}

# find_comment prints the id of the comment carrying the marker in the
# listing file $1, or nothing. The API's JSON is pretty-printed: a comment's
# own id is at four spaces, its body is one line.
find_comment() {
	awk -v marker="$marker" '
		/^    "id": [0-9]+,?$/ { id = $2; sub(/,$/, "", id) }
		/^    "body": / && index($0, marker) { print id; exit }
	' "$1"
}

args=(check --format md)
if [ "${ASTIMATE_ALL:-false}" = true ]; then
	args+=(--all)
fi
if [ -n "${ASTIMATE_CONFIG:-}" ]; then
	args+=(--config "$ASTIMATE_CONFIG")
fi
if [ -n "${ASTIMATE_BASE:-}" ]; then
	args+=(--base "$ASTIMATE_BASE")
fi
path="${ASTIMATE_PATH:-.}"
md=$(astimate "${args[@]}" -- "$path")
status=$?
case "$status" in
0 | 3) ;;
*)
	warn "astimate check exited $status; no comment posted"
	exit 0
	;;
esac

body="$marker"$'\n'"$md"
if [ "${ASTIMATE_HTML:-false}" = true ]; then
	body+=$'\n'"Full report: \`astimate-report.html\` in the \`astimate-report\` artifact of this run."
fi
body+=$'\n'

dry="${ASTIMATE_COMMENT_DRY_RUN:-}"
pr="${ASTIMATE_PR_NUMBER:-}"
repo="${GITHUB_REPOSITORY:-}"
api="${GITHUB_API_URL:-https://api.github.com}"
if [ -z "$pr" ] || [ -z "$repo" ]; then
	warn "no pull request number or repository; no comment posted"
	exit 0
fi
if [ -z "$dry" ] && [ -z "${GITHUB_TOKEN:-}" ]; then
	warn "no GITHUB_TOKEN; no comment posted (the job needs pull-requests: write)"
	exit 0
fi
url="$api/repos/$repo/issues/$pr/comments"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# api_call runs curl against the API with the token and prints the HTTP
# status; the response body is left in $work/response.
api_call() {
	curl -sS -o "$work/response" -w '%{http_code}' \
		-H "Authorization: Bearer $GITHUB_TOKEN" \
		-H "Accept: application/vnd.github+json" \
		-H "X-GitHub-Api-Version: 2022-11-28" "$@"
}

id=""
if [ -n "$dry" ]; then
	if [ -n "${ASTIMATE_COMMENTS_FILE:-}" ]; then
		id=$(find_comment "$ASTIMATE_COMMENTS_FILE")
	fi
else
	for page in 1 2 3 4 5 6 7 8 9 10; do
		code=$(api_call "$url?per_page=100&page=$page") || code=000
		if [ "$code" != 200 ]; then
			warn "listing the pull request's comments failed (HTTP $code); no comment posted so as never to post twice"
			exit 0
		fi
		id=$(find_comment "$work/response")
		if [ -n "$id" ] || ! grep -q '^    "id": ' "$work/response"; then
			break
		fi
	done
fi

if [ -n "$id" ]; then
	method=PATCH
	target="$api/repos/$repo/issues/comments/$id"
else
	method=POST
	target="$url"
fi
json=$(json_body "$body")
if [ -n "$dry" ]; then
	echo "$method $target"
	echo "$json"
	exit 0
fi
code=$(printf '%s' "$json" | api_call -X "$method" -d @- "$target") || code=000
case "$code" in
200 | 201) ;;
*) warn "$method $target failed (HTTP $code); the job needs pull-requests: write, and a pull request from a fork cannot comment" ;;
esac
exit 0
