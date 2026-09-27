// Package engine orchestrates astimate's operations: it resolves a target
// directory to its module, configuration and extractor, and runs assess,
// rank, check and baseline write over it. It composes lang, baseline,
// score, gate, report and config so the CLI and the MCP server share one
// implementation; they parse their own inputs and render the results.
package engine
