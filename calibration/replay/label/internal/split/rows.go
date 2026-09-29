package split

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
)

// Commit is the part of a commits.jsonl row the rule reads.
type Commit struct {
	Commit          string   `json:"commit"`
	Subject         string   `json:"subject"`
	ConfigVersion   string   `json:"config_version"`
	Loaded          bool     `json:"loaded"`
	FilesChanged    []string `json:"files_changed"`
	PackagesChanged []string `json:"packages_changed"`
	PackagesDeleted []string `json:"packages_deleted"`
}

// Row is the part of a packages.jsonl row the rule reads.
type Row struct {
	Commit    string              `json:"commit"`
	Package   string              `json:"package"`
	Language  string              `json:"language"`
	Metrics   metrics.RawMetrics  `json:"metrics"`
	Base      *metrics.RawMetrics `json:"base"`
	SLOCDelta int                 `json:"sloc_delta"`
}

// module reports whether r is the module row.
func (r *Row) module() bool { return r.Package == metrics.ModuleRowID }

// baseSLOC is r's sloc at the parent, 0 for a new row.
func (r *Row) baseSLOC() int {
	if r.Base == nil {
		return 0
	}
	return r.Base.SLOC
}

// dups returns the duplicate count the rule follows on r in m, one of r's
// head or base metrics: dup_blocks_cross_pkg on the module row, dup_blocks
// on a package row; 0 when m is nil or has none.
func (r *Row) dups(m *metrics.RawMetrics) int {
	switch {
	case m == nil:
		return 0
	case !r.module():
		return m.DupBlocks
	case m.DupBlocksCrossPkg != nil:
		return *m.DupBlocksCrossPkg
	}
	return 0
}

// event is one point of a package's timeline: its row at commit index at,
// or its deletion there when row is nil.
type event struct {
	at  int
	row *Row
}

// Replay is a replay's commits with their rows, indexed for the rule.
type Replay struct {
	// Commits are the replayed first-parent commits, in order.
	Commits []Commit
	// rows holds each commit's rows, module row included.
	rows [][]Row
	// timeline holds each package's rows and deletion, in commit order.
	timeline map[string][]event
	// moduleDir is the module root relative to the repository's top level,
	// "." for the top level; files_changed paths are relative to the top.
	moduleDir string
	// capacity returns the capacity rules that judge a row of a language.
	capacity func(lang string) []gate.Threshold
}

// NewReplay indexes commits and their rows (keyed by commit hash). The
// module lives at moduleDir; capacity returns the capacity rules of a
// language.
func NewReplay(commits []Commit, rows map[string][]Row, moduleDir string, capacity func(lang string) []gate.Threshold) *Replay {
	r := &Replay{Commits: commits, rows: make([][]Row, len(commits)), timeline: map[string][]event{},
		moduleDir: moduleDir, capacity: capacity}
	for i := range commits {
		r.rows[i] = rows[commits[i].Commit]
		for k := range r.rows[i] {
			if row := &r.rows[i][k]; !row.module() {
				r.timeline[row.Package] = append(r.timeline[row.Package], event{at: i, row: row})
			}
		}
		for _, p := range commits[i].PackagesDeleted {
			r.timeline[p] = append(r.timeline[p], event{at: i})
		}
	}
	return r
}

// slocBefore returns package p's sloc at the parent of commit j: its base
// when commit j has a row for it, else its sloc after the nearest earlier
// row (0 after a deletion), else its base at the nearest later row; 0 when
// the rows never show it.
func (r *Replay) slocBefore(p string, j int) int {
	evs := r.timeline[p]
	k := sort.Search(len(evs), func(k int) bool { return evs[k].at >= j })
	switch {
	case k < len(evs) && evs[k].at == j && evs[k].row != nil:
		return evs[k].row.baseSLOC()
	case k > 0 && evs[k-1].row == nil:
		return 0
	case k > 0:
		return evs[k-1].row.Metrics.SLOC
	case k < len(evs) && evs[k].row != nil:
		return evs[k].row.baseSLOC()
	}
	return 0
}

// moduleFall returns how much of the module's total sloc commit j
// deleted, and the total at its parent. The total counts only packages the
// rows show.
func (r *Replay) moduleFall(j int) (fall, total int) {
	for p := range r.timeline {
		total += r.slocBefore(p, j)
	}
	for k := range r.rows[j] {
		if row := &r.rows[j][k]; !row.module() {
			fall += row.baseSLOC() - row.Metrics.SLOC
		}
	}
	for _, p := range r.Commits[j].PackagesDeleted {
		fall += r.slocBefore(p, j)
	}
	return fall, total
}

// rowOf returns commit j's row for package p, or nil.
func (r *Replay) rowOf(j int, p string) *Row {
	for k := range r.rows[j] {
		if r.rows[j][k].Package == p {
			return &r.rows[j][k]
		}
	}
	return nil
}

// touches reports whether commit j changed a file directly in the
// directory of one of the packages pkgs (module-relative).
func (r *Replay) touches(j int, pkgs ...string) bool {
	for _, f := range r.Commits[j].FilesChanged {
		rel := f
		if r.moduleDir != "." && r.moduleDir != "" {
			var ok bool
			if rel, ok = strings.CutPrefix(f, r.moduleDir+"/"); !ok {
				continue
			}
		}
		dir := path.Dir(rel)
		for _, p := range pkgs {
			if dir == p {
				return true
			}
		}
	}
	return false
}

// Run is the source run.json records for a replay.
type Run struct {
	// Range is the revision range replayed.
	Range string `json:"range"`
	// Remote is the replayed repository's remote, Repository its name.
	Remote     string `json:"remote"`
	Repository string `json:"repository"`
	// ModuleDir is the module root relative to the repository's top level.
	ModuleDir string `json:"module_dir"`
}

// ReadRun reads the source of the replay data directory dir's run.json.
// An empty ModuleDir reads as ".".
func ReadRun(dir string) (Run, error) {
	var run struct {
		Source Run `json:"source"`
	}
	data, err := os.ReadFile(filepath.Join(dir, "run.json"))
	if err == nil {
		err = json.Unmarshal(data, &run)
	}
	if err != nil {
		return Run{}, fmt.Errorf("reading the replay's run.json: %w", err)
	}
	if run.Source.ModuleDir == "" {
		run.Source.ModuleDir = "."
	}
	return run.Source, nil
}

// Load reads the replay data directory dir: its run, commits and package
// rows, with the capacity rules of the configuration at cfg, or, when cfg
// is empty, of the one the replay gated with.
func Load(dir, cfg string) (*Replay, Run, error) {
	run, err := ReadRun(dir)
	if err != nil {
		return nil, run, err
	}
	commits, err := decodeAll[Commit](filepath.Join(dir, "commits.jsonl"))
	if err == nil && len(commits) == 0 {
		err = fmt.Errorf("%s/commits.jsonl has no commits", dir)
	}
	if err != nil {
		return nil, run, err
	}
	all, err := decodeAll[Row](filepath.Join(dir, "packages.jsonl"))
	if err != nil {
		return nil, run, err
	}
	rows := make(map[string][]Row, len(commits))
	for _, row := range all {
		rows[row.Commit] = append(rows[row.Commit], row)
	}
	c, err := replayConfig(cfg, commits[0].ConfigVersion)
	if err != nil {
		return nil, run, err
	}
	return NewReplay(commits, rows, run.ModuleDir, capacityOf(c)), run, nil
}

// capacityOf returns, for a language, the capacity rules cfg judges its
// rows with.
func capacityOf(cfg *config.Config) func(lang string) []gate.Threshold {
	return func(lang string) []gate.Threshold {
		var out []gate.Threshold
		for _, t := range cfg.ForLanguage(lang).Thresholds {
			if t.Kind == gate.Capacity {
				out = append(out, t)
			}
		}
		return out
	}
}

// replayConfig returns the configuration at path, or, when path is empty,
// the one the replay gated with: the embedded default when its version is
// version, else the committed candidate
// calibration/thresholds/astimate-<version>.yaml. Its config_version must
// be version.
func replayConfig(path, version string) (*config.Config, error) {
	if path == "" {
		def, err := config.Parse(config.Default())
		if err == nil && def.Version == version {
			return def, nil
		}
		path = filepath.Join("calibration", "thresholds", "astimate-"+version+".yaml")
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, fmt.Errorf("loading the configuration the replay gated with: %w", err)
	}
	if cfg.Version != version {
		return nil, fmt.Errorf("%s is %s, but the replay gated with %s", path, cfg.Version, version)
	}
	return cfg, nil
}

// decodeAll decodes every JSON value of the JSON Lines file at path.
func decodeAll[T any](path string) ([]T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading the replay: %w", err)
	}
	var out []T
	for dec := json.NewDecoder(bytes.NewReader(data)); dec.More(); {
		var v T
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("decoding %s record %d: %w", path, len(out)+1, err)
		}
		out = append(out, v)
	}
	return out, nil
}
