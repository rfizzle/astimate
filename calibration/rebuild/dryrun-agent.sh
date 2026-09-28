#!/bin/sh
# Stand-in agent for a dry run of `go run ./calibration/rebuild run`. It
# spends no requests: it restores the package's original implementation
# from the clone's git history and prints the stream-json lines Claude Code
# would, with zero tokens, one turn and one tool call, so the oracle passes
# and every field of the row is filled. Usage, from the repository root:
#
#   go run ./calibration/rebuild run --agent "sh $PWD/calibration/rebuild/dryrun-agent.sh {dir}" ...
#
# The runner starts it with the module root as its working directory.
set -eu
dir="$1"
git checkout HEAD -- "$dir"
printf '%s\n' '{"type":"system","subtype":"init","session_id":"dry-run","model":"dry-run"}'
printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_dry_run","name":"Bash","input":{"command":"git checkout HEAD -- '"$dir"'"}}]},"session_id":"dry-run"}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"duration_ms":0,"duration_api_ms":0,"num_turns":1,"session_id":"dry-run","total_cost_usd":0,"usage":{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0},"modelUsage":{"dry-run":{"inputTokens":0,"outputTokens":0,"cacheReadInputTokens":0,"cacheCreationInputTokens":0,"costUSD":0}}}'
