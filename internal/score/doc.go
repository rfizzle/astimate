// Package score estimates the effort to rebuild a package from its tests and
// exported contract, per SPEC.md section 7. The estimate sums five token terms
// (volume, spec, contract, unspecified behavior and hidden state) derived from
// a package's raw metrics, and converts the total into agent passes against a
// context budget and into a COCOMO-based human estimate in working days. Every
// parameter comes from the rebuild section of the config as RebuildParams.
package score
