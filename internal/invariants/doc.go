// Package invariants holds the acceptance invariants of SPEC.md section 7.5
// and the gate guarantees of section 8.1 as tests that every configuration
// must pass. It has no non-test code; it exists so the suite can import
// score, gate, config and the Go extractor together without creating an
// import cycle, and so that one command checks a configuration:
//
//	ASTIMATE_CONFIG=path/to/astimate.yaml go test ./internal/invariants
//
// go test runs in the package directory, so give an absolute path or one
// relative to internal/invariants. With ASTIMATE_CONFIG unset the embedded
// default configuration is used. The
// suite checks that the standard-library errors package is ONE_PASS and
// net/http is PARTITION (skipped when the toolchain cannot load them), that a
// small tested package with no fan-in or duplication is ONE_PASS, that
// agent_passes moves monotonically in the directions section 7.5 names, that
// identical head and baseline trees never violate a density rule with a
// non-negative max_delta or a requirement (only a capacity ceiling the
// baseline already breached may fail them, since capacity rules are
// absolute), and that growing capacity metrics below their max with density
// metrics unchanged and requirements met never violates any rule. A calibrated
// configuration that breaks any of these is rejected before it ships.
package invariants
