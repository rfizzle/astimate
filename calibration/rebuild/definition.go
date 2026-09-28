package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// StubSignatures is the one stub strategy: every function and method body
// of the package's non-test files becomes panic("not implemented"), and
// everything else (package clause, imports still used, types, constants,
// variables, signatures, doc comments, build constraints) is kept.
const StubSignatures = "signatures"

// minExperiments is the smallest experiment set SPEC.md 11.2 allows.
const minExperiments = 30

// errInvalidDefinition is wrapped by every error Definition.Validate
// returns.
var errInvalidDefinition = errors.New("invalid rebuild definition")

// Definition is the rebuild experiment definition, rebuild.yaml: the
// packages SPEC.md 11.2 rebuilds, how each is stubbed and checked, and the
// estimate each carried before the run.
type Definition struct {
	// Note explains the file; it carries no data.
	Note string `yaml:"note"`
	// Source is the packages.jsonl the metrics and estimates were copied
	// from, relative to the repository root.
	Source string `yaml:"source"`
	// ConfigVersion is the config_version the estimates were made under.
	ConfigVersion string `yaml:"config_version"`
	// GoVersion is the toolchain the selection was verified with and the
	// stub hashes were computed under.
	GoVersion string `yaml:"go_version"`
	// Env is the environment, as KEY=VALUE, every go command of the
	// experiment runs with (stub checks and the oracle alike).
	Env []string `yaml:"env"`
	// Selection records the rule the experiments were chosen by.
	Selection SelectionRule `yaml:"selection"`
	// Experiments are the packages to rebuild, in selection order.
	Experiments []Experiment `yaml:"experiments"`
}

// SelectionRule holds the parameters of the deterministic selection rule
// described in calibration/rebuild/README.md.
type SelectionRule struct {
	// PerStratum is the number of packages taken from each stratum (tier
	// by has_tests).
	PerStratum int `yaml:"per_stratum"`
	// MaxPerModule bounds how many packages one module contributes.
	MaxPerModule int `yaml:"max_per_module"`
	// MaxAgentPasses leaves out packages whose estimate is above it, which
	// bounds what one experiment spends.
	MaxAgentPasses float64 `yaml:"max_agent_passes"`
}

// Experiment is one package to delete and rebuild.
type Experiment struct {
	// Module is the module path from go.mod.
	Module string `yaml:"module"`
	// Repo is the git URL the module is cloned from.
	Repo string `yaml:"repo"`
	// Commit is the full commit hash the module is pinned at in
	// calibration/corpus.yaml.
	Commit string `yaml:"commit"`
	// Package is the import path of the package to rebuild.
	Package string `yaml:"package"`
	// Dir is the package directory relative to the module root, in slash
	// form; "." for the root package.
	Dir string `yaml:"dir"`
	// Stub is the stub strategy; StubSignatures is the only one.
	Stub string `yaml:"stub"`
	// StubSHA256 is TreeHash of the stubbed files, so a runner can check it
	// starts from the tree the selection verified.
	StubSHA256 string `yaml:"stub_sha256"`
	// Oracle decides whether a rebuild succeeded.
	Oracle Oracle `yaml:"oracle"`
	// TurnCap is the most agent turns one run may take.
	TurnCap int `yaml:"turn_cap"`
	// HasTests is the package's own has_tests; when false the oracle runs
	// the tests of the package's importers instead.
	HasTests bool `yaml:"has_tests"`
	// Tier is the estimate's tier before the run.
	Tier score.Tier `yaml:"tier"`
	// AgentPasses is the estimate before the run, rounded as rank reports
	// it.
	AgentPasses float64 `yaml:"agent_passes"`
	// RebuildTokens is the estimate's rebuild_tokens before the run,
	// rounded to an integer.
	RebuildTokens int `yaml:"rebuild_tokens"`
	// HumanDays is the human estimate before the run, rounded as rank
	// reports it.
	HumanDays float64 `yaml:"human_days"`
	// Metrics are the package's raw metrics at the pin.
	Metrics Metrics `yaml:"metrics"`
}

// Oracle is the check a rebuild must pass, run from the module root:
// `go test <Test...> && go build <Build...>`.
type Oracle struct {
	// Test are the package patterns whose tests must pass: the package
	// itself when it has tests, else its in-module importers that have
	// tests.
	Test []string `yaml:"test"`
	// Build are the package patterns that must still compile, always
	// "./...", which covers every importer in the module.
	Build []string `yaml:"build"`
}

// Command renders the oracle as the shell command it stands for, for logs
// and documentation; a runner executes the two go commands directly.
func (o Oracle) Command() string {
	return "go test " + strings.Join(o.Test, " ") + " && go build " + strings.Join(o.Build, " ")
}

// Metrics wraps metrics.RawMetrics so it reads and writes YAML under the
// JSON field names packages.jsonl and the report schema use.
type Metrics struct {
	metrics.RawMetrics
}

// MarshalYAML writes the metrics as a block mapping in the JSON field order.
func (m Metrics) MarshalYAML() (any, error) {
	data, err := json.Marshal(m.RawMetrics)
	if err != nil {
		return nil, fmt.Errorf("encoding metrics: %w", err)
	}
	// JSON is YAML, and decoding it into a node keeps the key order.
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("encoding metrics: %w", err)
	}
	n := doc.Content[0]
	plainStyle(n)
	return n, nil
}

// plainStyle clears the flow and quoting styles decoding JSON left on n and
// its children, so the encoder writes block style with plain scalars.
func plainStyle(n *yaml.Node) {
	n.Style = 0
	for _, c := range n.Content {
		plainStyle(c)
	}
}

// UnmarshalYAML reads the metrics strictly, rejecting unknown fields.
func (m *Metrics) UnmarshalYAML(n *yaml.Node) error {
	var v any
	if err := n.Decode(&v); err != nil {
		return fmt.Errorf("decoding metrics: %w", err)
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("decoding metrics: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m.RawMetrics); err != nil {
		return fmt.Errorf("decoding metrics: %w", err)
	}
	return nil
}

// LoadDefinition reads and validates the definition file at path.
func LoadDefinition(path string) (*Definition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading definition: %w", err)
	}
	d, err := ParseDefinition(data)
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", path, err)
	}
	return d, nil
}

// ParseDefinition decodes a definition strictly, rejecting unknown keys,
// and validates it.
func ParseDefinition(data []byte) (*Definition, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var d Definition
	if err := dec.Decode(&d); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%w: empty document", errInvalidDefinition)
		}
		return nil, fmt.Errorf("decoding definition: %w", err)
	}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return &d, nil
}

// Marshal encodes the definition as YAML with two-space indentation.
func (d *Definition) Marshal() ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(d); err != nil {
		return nil, fmt.Errorf("encoding definition: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encoding definition: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate checks the definition: the header fields, every experiment, no
// package twice, at least minExperiments experiments, and a selection that
// has tested and untested packages in every tier. The returned
// error joins one error per problem, each wrapping errInvalidDefinition.
func (d *Definition) Validate() error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("%w: "+format, append([]any{errInvalidDefinition}, args...)...))
	}
	if d.Source == "" {
		fail("source is required")
	}
	if d.ConfigVersion == "" {
		fail("config_version is required")
	}
	if !strings.HasPrefix(d.GoVersion, "go1.") {
		fail("go_version %q is not a Go release such as go1.27.1", d.GoVersion)
	}
	for _, kv := range d.Env {
		if k, _, ok := strings.Cut(kv, "="); !ok || k == "" {
			fail("env entry %q is not KEY=VALUE", kv)
		}
	}
	if d.Selection.PerStratum <= 0 || d.Selection.MaxPerModule <= 0 || d.Selection.MaxAgentPasses <= 0 {
		fail("selection per_stratum, max_per_module and max_agent_passes must be > 0")
	}
	if len(d.Experiments) < minExperiments {
		fail("want at least %d experiments, got %d", minExperiments, len(d.Experiments))
	}
	seen := make(map[string]bool, len(d.Experiments))
	covered := make(map[stratum]bool)
	for i := range d.Experiments {
		e := &d.Experiments[i]
		for _, err := range e.validate() {
			fail("experiments[%d] %s: %w", i, e.Package, err)
		}
		if seen[e.Package] {
			fail("experiments[%d]: duplicate package %s", i, e.Package)
		}
		seen[e.Package] = true
		covered[stratum{e.Tier, e.HasTests}] = true
	}
	for _, s := range strata() {
		if !covered[s] {
			fail("no %s experiment; every tier needs tested and untested packages", s)
		}
	}
	return errors.Join(errs...)
}

// tiers returns the SPEC.md 7.4 tiers in order.
func tiers() []score.Tier {
	return []score.Tier{score.TierOnePass, score.TierFewPasses, score.TierPartition}
}

// validate returns one error per problem with the experiment.
func (e *Experiment) validate() []error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}
	switch {
	case e.Module == "" || e.Module == stdlibModule:
		fail("module %q must be a cloned corpus module", e.Module)
	case e.Package != e.Module && !strings.HasPrefix(e.Package, e.Module+"/"):
		fail("package is not in module %s", e.Module)
	case e.Dir != modRelDir(e.Module, e.Package):
		fail("dir %q does not match the package path (want %q)", e.Dir, modRelDir(e.Module, e.Package))
	}
	if !strings.HasPrefix(e.Repo, "https://") {
		fail("repo %q is not an https URL", e.Repo)
	}
	if !isHex(e.Commit, 40) {
		fail("commit %q is not a full 40-character hex hash", e.Commit)
	}
	if e.Stub != StubSignatures {
		fail("stub %q is not %q", e.Stub, StubSignatures)
	}
	if !isHex(e.StubSHA256, 64) {
		fail("stub_sha256 %q is not a 64-character hex hash", e.StubSHA256)
	}
	errs = append(errs, e.Oracle.validate(e.Dir, e.HasTests)...)
	if e.TurnCap <= 0 {
		fail("turn_cap must be > 0, got %d", e.TurnCap)
	}
	if !slices.Contains(tiers(), e.Tier) {
		fail("tier %q is not one of %v", e.Tier, tiers())
	}
	if e.AgentPasses < 0 || e.RebuildTokens <= 0 || e.HumanDays < 0 {
		fail("rebuild_tokens must be > 0 and agent_passes and human_days >= 0")
	}
	if err := e.Metrics.Validate(); err != nil {
		errs = append(errs, err)
	}
	if e.HasTests != e.Metrics.HasTests {
		fail("has_tests %v disagrees with metrics.has_tests %v", e.HasTests, e.Metrics.HasTests)
	}
	if e.Metrics.UsesCgo == nil || *e.Metrics.UsesCgo {
		fail("uses_cgo must be false")
	}
	return errs
}

// validate checks the oracle of the package in dir: tests given as
// module-relative patterns, the package's own directory when it has tests
// and never when it has none, and a build of the whole module.
func (o Oracle) validate(dir string, hasTests bool) []error {
	var errs []error
	own := "./" + dir
	if dir == "." {
		own = "."
	}
	if len(o.Test) == 0 {
		errs = append(errs, errors.New("oracle.test is empty"))
	}
	for _, p := range o.Test {
		if p != "." && !strings.HasPrefix(p, "./") {
			errs = append(errs, fmt.Errorf("oracle.test pattern %q is not module-relative", p))
		}
	}
	switch {
	case hasTests && !slices.Equal(o.Test, []string{own}):
		errs = append(errs, fmt.Errorf("oracle.test of a tested package must be [%s], got %v", own, o.Test))
	case !hasTests && slices.Contains(o.Test, own):
		errs = append(errs, fmt.Errorf("oracle.test of an untested package must list its importers, not %s", own))
	}
	if !slices.IsSorted(o.Test) {
		errs = append(errs, errors.New("oracle.test is not sorted"))
	}
	if !slices.Equal(o.Build, []string{"./..."}) {
		errs = append(errs, fmt.Errorf("oracle.build must be [./...], got %v", o.Build))
	}
	return errs
}

// modRelDir returns the directory of pkg relative to the root of module, in
// slash form, "." for the root package.
func modRelDir(module, pkg string) string {
	if pkg == module {
		return "."
	}
	return strings.TrimPrefix(pkg, module+"/")
}

// isHex reports whether s is n lower-case hex digits.
func isHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
