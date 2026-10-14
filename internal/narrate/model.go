package narrate

import "github.com/rfizzle/astimate/internal/report"

// Band names, the three verdicts a package gets (SPEC.md 10.3).
const (
	// BandSplit is a package past one agent pass or over two ceilings.
	BandSplit = "split"
	// BandWatch is a package over one ceiling, needing a few passes or
	// a quarter copied.
	BandWatch = "watch"
	// BandFine is every other package.
	BandFine = "fine"
)

// Module is the module report: the whole repository, and the change
// against a base when one was given.
type Module struct {
	// ModulePath is the module's path; empty for a module without one.
	ModulePath string `json:"module_path"`
	// Name is the module's short name the sentences use.
	Name string `json:"name"`
	// Language is the extractor's language.
	Language string `json:"language"`
	// Head is the commit the report describes; empty outside git.
	Head string `json:"head,omitempty"`
	// Limits are the ceilings the bands are judged against.
	Limits []Limit `json:"limits"`
	// ChurnWindow is the commits churn counts over; 0 when churn is off.
	ChurnWindow int `json:"churn_window"`
	// Counts are the module totals.
	Counts Counts `json:"counts"`
	// Headline is the one-sentence verdict on the repository.
	Headline string `json:"headline"`
	// Lead is the paragraph under Headline.
	Lead string `json:"lead"`
	// Hotspots are the paths of the five split or watch packages with the
	// highest hotspot score, highest first (by rebuild days without
	// churn); "Start here" is the first three. A fine package is never
	// one: there is nothing to do about it.
	Hotspots []string `json:"hotspots"`
	// Packages are every package, by path.
	Packages []Package `json:"packages"`
	// Change is the change against the base; nil without one.
	Change *Change `json:"change,omitempty"`
	// Glossary explains the words the report uses.
	Glossary []Term `json:"glossary"`
	// AstimateVersion is the astimate version that built the report.
	AstimateVersion string `json:"astimate_version"`
	// ConfigVersion is the configuration's config_version.
	ConfigVersion string `json:"config_version"`
}

// Limit is one ceiling a package is judged against: a capacity rule's max.
type Limit struct {
	// Metric is the RawMetrics field the ceiling applies to.
	Metric string `json:"metric"`
	// Max is the ceiling.
	Max float64 `json:"max"`
	// Roles are the package roles the rule judges; empty means all.
	Roles []string `json:"roles,omitempty"`
}

// Counts are the module totals.
type Counts struct {
	// Packages is the number of packages.
	Packages int `json:"packages"`
	// Split, Watch and Fine count the packages in each band.
	Split int `json:"split"`
	Watch int `json:"watch"`
	Fine  int `json:"fine"`
	// WithoutTests counts the packages with no tests.
	WithoutTests int `json:"without_tests"`
	// HumanDays is the sum of every package's human_days, rounded.
	HumanDays float64 `json:"human_days"`
	// HumanTime is HumanDays as one engineer's working time.
	HumanTime string `json:"human_time"`
}

// Package is one package's view.
type Package struct {
	// Path is the package directory relative to the module root.
	Path string `json:"path"`
	// Role is the package's role; empty when the extractor records none.
	Role string `json:"role,omitempty"`
	// Tier is the rebuild tier.
	Tier string `json:"tier"`
	// Band is BandSplit, BandWatch or BandFine.
	Band string `json:"band"`
	// Over are the metrics of the ceilings the package is over.
	Over []string `json:"over"`
	// Hotspot is churn_commits times human_days; 0 without churn.
	Hotspot float64 `json:"hotspot"`
	// Ceilings are the package's values against each limit.
	Ceilings []Ceiling `json:"ceilings"`
	// Rebuild is the rebuild view.
	Rebuild Rebuild `json:"rebuild"`
	// Copies is the copied-code view.
	Copies Copies `json:"copies"`
	// Complexity is the per-function complexity view.
	Complexity Complexity `json:"complexity"`
	// Tests is the tests view.
	Tests Tests `json:"tests"`
	// Churn is the package's churn; nil when it was not measured.
	Churn *Churn `json:"churn"`
	// FanIn is the number of module packages importing it.
	FanIn int `json:"fan_in"`
	// Story says what is wrong with the package, in one sentence.
	Story string `json:"story"`
	// Todo says what to do about it.
	Todo string `json:"todo"`
	// Changes says how often it changed; empty without churn.
	Changes string `json:"changes,omitempty"`
	// Facts are the labelled one-line facts of the package's detail.
	Facts []Fact `json:"facts"`
}

// Ceiling is a package's value against one limit.
type Ceiling struct {
	// Metric is the limit's metric.
	Metric string `json:"metric"`
	// Label names the metric for people.
	Label string `json:"label"`
	// Value is the package's value.
	Value float64 `json:"value"`
	// Max is the ceiling.
	Max float64 `json:"max"`
	// Judged is false when the rule does not judge the package's role.
	Judged bool `json:"judged"`
	// Over is true when the rule judges the package and Value exceeds Max.
	Over bool `json:"over"`
	// Level colours the meter: split over, watch at 80% or more, else fine.
	Level string `json:"level"`
}

// Rebuild is the rebuild view.
type Rebuild struct {
	// AgentPasses and HumanDays are the estimate's.
	AgentPasses float64 `json:"agent_passes"`
	HumanDays   float64 `json:"human_days"`
	// HumanTime is HumanDays as one engineer's working time; HumanShort
	// the same in a table cell's units.
	HumanTime  string `json:"human_time"`
	HumanShort string `json:"human_short"`
	// RebuildTokens is the estimate's rebuild_tokens.
	RebuildTokens int `json:"rebuild_tokens"`
	// Sittings is the sittings band: one sitting, a few sittings or split
	// it first.
	Sittings string `json:"sittings"`
	// SittingsText is the passes as sittings an agent needs.
	SittingsText string `json:"sittings_text"`
	// Drivers are the estimate's terms as shares of RebuildTokens.
	Drivers []Share `json:"drivers"`
	// Say is the view's sentence.
	Say string `json:"say"`
}

// Share is one rebuild driver as a share of rebuild_tokens.
type Share struct {
	// Term is the driver's term.
	Term string `json:"term"`
	// Label says what the term is; empty for a term the view lumps
	// into the rest.
	Label string `json:"label,omitempty"`
	// Tokens is the term's tokens.
	Tokens int `json:"tokens"`
	// Pct is Tokens as a whole percentage of rebuild_tokens.
	Pct int `json:"pct"`
}

// Copies is the copied-code view.
type Copies struct {
	// Pct is duplication_pct.
	Pct float64 `json:"pct"`
	// Blocks is dup_blocks; CodeBlocks dup_blocks_code, nil when not
	// recorded.
	Blocks     int  `json:"blocks"`
	CodeBlocks *int `json:"code_blocks"`
	// Lines is the package's sloc.
	Lines int `json:"lines"`
	// Level colours the share: split at 25%, watch at 10%, else fine.
	Level string `json:"level"`
	// Chunks are two occurrences of the longest duplicate block.
	Chunks []Chunk `json:"chunks"`
	// Say is the view's sentence.
	Say string `json:"say"`
	// Longest names Chunks; empty without any.
	Longest string `json:"longest,omitempty"`
}

// Chunk is one occurrence of a duplicate block.
type Chunk struct {
	// File is relative to the package directory.
	File string `json:"file"`
	// Start and End are the first and last line.
	Start int `json:"start"`
	End   int `json:"end"`
	// Lines is End-Start+1.
	Lines int `json:"lines"`
}

// Complexity is the per-function complexity view.
type Complexity struct {
	// Functions is the number of functions.
	Functions int `json:"functions"`
	// Buckets count the functions by cognitive complexity; nil when the
	// extractor recorded no per-function values.
	Buckets *Buckets `json:"buckets"`
	// CognitiveP90, CyclomaticP90 and Nesting are the package's.
	CognitiveP90  int `json:"cognitive_p90"`
	CyclomaticP90 int `json:"cyclomatic_p90"`
	Nesting       int `json:"nesting"`
	// Hardest are the three functions with the highest cognitive
	// complexity.
	Hardest []Function `json:"hardest"`
	// Say is the view's sentence; empty for a package with no functions.
	Say string `json:"say,omitempty"`
}

// Buckets count functions by cognitive complexity: easy up to 10, care
// 11 to 20, hard over 20.
type Buckets struct {
	Easy int `json:"easy"`
	Care int `json:"care"`
	Hard int `json:"hard"`
}

// Function is one of the hardest functions.
type Function struct {
	// Name is the function, receiver-prefixed for a method.
	Name string `json:"name"`
	// File is relative to the package directory; Line its declaration.
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
	// Cognitive and Cyclomatic are its complexities.
	Cognitive  int `json:"cognitive"`
	Cyclomatic int `json:"cyclomatic"`
	// Level is the bucket band: split hard, watch care, fine easy.
	Level string `json:"level"`
}

// Tests is the tests view.
type Tests struct {
	TestFuncs int  `json:"test_funcs"`
	HasTests  bool `json:"has_tests"`
	// Untested is untested_exports, of Exports exported symbols.
	Untested int `json:"untested"`
	Exports  int `json:"exports"`
}

// Churn is a package's churn over the window.
type Churn struct {
	Commits int `json:"commits"`
	Authors int `json:"authors"`
}

// Fact is a labelled one-line fact.
type Fact struct {
	Label string `json:"label"`
	Text  string `json:"text"`
}

// Term is one glossary entry.
type Term struct {
	Term string `json:"term"`
	Text string `json:"text"`
}

// Change is the change against a base: the check of the changed
// packages.
type Change struct {
	// Base is the ref given; BaseCommit the baseline's commit.
	Base       string `json:"base"`
	BaseCommit string `json:"base_commit,omitempty"`
	// Verdict is FAIL or PASS.
	Verdict string `json:"verdict"`
	// Headline and Lead are the ribbon's sentences.
	Headline string `json:"headline"`
	Lead     string `json:"lead"`
	// Changed, Failed, Passed and Unchanged count packages; Violations
	// and Warnings findings.
	Changed    int `json:"changed"`
	Failed     int `json:"failed"`
	Violations int `json:"violations"`
	Warnings   int `json:"warnings"`
	Passed     int `json:"passed"`
	Unchanged  int `json:"unchanged"`
	// Cross is the module's cross-package copies at base and head; nil
	// without a module row.
	Cross *Cross `json:"cross"`
	// Failing are the packages with violations, most first.
	Failing []Failing `json:"failing"`
	// WarningGroups are the warnings grouped by metric, largest first.
	WarningGroups []WarningGroup `json:"warning_groups"`
	// PassedPaths are the changed packages with no finding.
	PassedPaths []string `json:"passed_paths"`
	// PassedSummary says how many passed and how many did not change.
	PassedSummary string `json:"passed_summary"`
	// Deleted are packages the baseline had and head does not.
	Deleted []string `json:"deleted,omitempty"`
	// Exempted are the violations an exemption accepted, each with its
	// reason (SPEC.md 8.6); omitted when there are none.
	Exempted []Exempted `json:"exempted,omitempty"`
	// Notices are the check's notices, one sentence each: a package whose
	// baseline is unavailable, or that was skipped because it does not
	// load (SPEC.md 8.5); omitted when there are none.
	Notices []string `json:"notices,omitempty"`
}

// Exempted is a violation an exemption accepted.
type Exempted struct {
	// Path is the package path, or <module> for the module row.
	Path string `json:"path"`
	// Metric is the rule's metric; Label the same for people.
	Metric string `json:"metric"`
	Label  string `json:"label"`
	// Reason is the exemption's recorded reason.
	Reason string `json:"reason"`
}

// Cross is dup_blocks_cross_pkg at base and head.
type Cross struct {
	Base *float64 `json:"base"`
	Head float64  `json:"head"`
}

// Failing is a package, or the module row, with violations.
type Failing struct {
	// Path is the package path, or <module> for the module row.
	Path string `json:"path"`
	// Summary counts its findings with its rebuild days and churn.
	Summary string `json:"summary"`
	// Findings are its violations.
	Findings []Finding `json:"findings"`
}

// Finding is one violation as the change report shows it.
type Finding struct {
	// Metric is the rule's metric; Label the same for people.
	Metric string `json:"metric"`
	Label  string `json:"label"`
	// Base and Head are the values; Base nil for a new package.
	Base *float64 `json:"base"`
	Head float64  `json:"head"`
	// OverBy and Allowed are parsed from an over-max limit: how far over
	// the ceiling head is and how much one change may grow; nil otherwise.
	OverBy  *float64 `json:"over_by"`
	Allowed *float64 `json:"allowed"`
	// Limit is the gate's limit text.
	Limit string `json:"limit"`
	// Why says why the gate cares; empty for a rule without one.
	Why string `json:"why"`
	// Do is the first clause of the gate's suggestion.
	Do string `json:"do"`
	// Moved says how far it moved against what is allowed; MovedShort the
	// same for a terminal line. Empty when the finding has no base.
	Moved      string `json:"moved,omitempty"`
	MovedShort string `json:"moved_short,omitempty"`
	// Location is the first place to look.
	Location *report.Location `json:"location,omitempty"`
}

// WarningGroup is the warnings of one metric.
type WarningGroup struct {
	Metric string `json:"metric"`
	Label  string `json:"label"`
	// Why says what the warning means.
	Why string `json:"why"`
	// Packages are the packages warned, by path.
	Packages []Warned `json:"packages"`
}

// Warned is one package's warning.
type Warned struct {
	Path string   `json:"path"`
	Base *float64 `json:"base"`
	Head float64  `json:"head"`
	// Limit is the gate's limit text and Do the first clause of its
	// suggestion, as on a Finding.
	Limit string `json:"limit"`
	Do    string `json:"do"`
}
