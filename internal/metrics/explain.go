package metrics

import "strings"

// Explanation describes one RawMetrics field for a reader deciding what a
// value means and whether it can fail the gate.
type Explanation struct {
	// Definition says what the metric counts, as in SPEC.md section 6.
	Definition string
	// Evidence summarizes why the metric is reported or gated, from SPEC.md
	// section 4.
	Evidence string
	// Release is "v0" or "v1", the SPEC.md section 6 release column.
	Release string
	// Gated reports whether the embedded default configuration has a
	// threshold on the metric. A config file may gate any metric; this is
	// the shipped default only.
	Gated bool
}

// Explain returns the explanation of the metric named by its JSON field
// name, and false when the name is not a RawMetrics field. Every name in
// MetricNames has an entry.
func Explain(name string) (Explanation, bool) {
	i := fieldIndex(name)
	if i < 0 {
		return Explanation{}, false
	}
	f := fieldTable()[i]
	evidence := explainText(f.evidence)
	if f.has("capacity") {
		evidence += " " + evidenceCapacity
	}
	return Explanation{
		Definition: explainText(f.definition),
		Evidence:   evidence,
		Release:    f.release,
		Gated:      f.has("gated"),
	}, true
}

// Evidence texts shared by more than one metric.
const (
	evidenceDuplication = "Copy-paste instead of extraction: industry reports on AI-assisted " +
		"repositories show rising duplicated blocks. The most specific LLM failure mode found; moderate evidence."
	evidenceBloat = "Package bloat: successful agent trajectories typically stay under 20 to 30k tokens, " +
		"resolve rates collapse at 64k tokens of context, and failed trajectories are longer. Strong evidence."
	evidenceCapacity = "Gated as a capacity rule: crossing the max, or a new package over it, fails. " +
		"A package already over the max warns while its value holds or falls, and may grow by at most the " +
		"rule's over_max_delta in one change, with a warning to split, before growth fails. The fix is a split, " +
		"not a smaller change."
	evidenceTests = "Untested additions: agents self-correct through a run-and-check loop, and a " +
		"package without tests denies the next agent that loop. Moderate, indirect evidence."
	evidenceHiddenState = "Hidden state: no direct study; plausible and kept at low weight."
	evidenceNesting     = "Deep nesting: classical complexity metrics show no consistent correlation with " +
		"LLM performance. Gated on regressions only; weak evidence."
	evidenceContext = "Context only: an input to the rebuild estimate or to other metrics, not a " +
		"failure mode the gate targets."
	evidenceCoupling = "Reported, not gated. Martin's package metrics are widely reported but their " +
		"validation as predictors is mixed and none exists for Go, whose consumer-defined interfaces " +
		"invert the abstract-provider assumption: idiomatic Go puts stable concrete leaf packages near " +
		"distance 1 by design. No gate rule until the reference-corpus measurement shows where " +
		"well-regarded Go modules sit."
	evidenceCoverage = "Opt-in and reported only: no gate rule, since baselines do not run tests " +
		"and a check measures head only, so there is no delta to gate."
	evidenceOpacity = "Opacity: code the next agent cannot follow from Go source alone (C, reflection, " +
		"unsafe memory, generator output). Informational: reported, not gated, and no default threshold."
)

// generatedExcluded ends the definition of every size and structure metric
// that counts only the files a person wrote (SPEC.md 6.5).
const generatedExcluded = " Generated files, those with a \"Code generated ... DO NOT EDIT.\" header, " +
	"are left out: a rebuild regenerates them; see tokens_est_generated."

// explainText expands the placeholders of a fieldTable text: {generated}
// to generatedExcluded and each {<name>} of the evidence texts above to
// its text.
func explainText(s string) string {
	return strings.NewReplacer(
		"{generated}", generatedExcluded,
		"{duplication}", evidenceDuplication,
		"{bloat}", evidenceBloat,
		"{tests}", evidenceTests,
		"{hidden}", evidenceHiddenState,
		"{nesting}", evidenceNesting,
		"{context}", evidenceContext,
		"{coupling}", evidenceCoupling,
		"{coverage}", evidenceCoverage,
		"{opacity}", evidenceOpacity,
	).Replace(s)
}
