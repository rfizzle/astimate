package blocklog

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/gate/exempt"
	"github.com/rfizzle/astimate/internal/report"
)

// coveredLimit ends the limit of an untested_exports finding judged on
// untested_exports_covered (SPEC.md 8.1, Source).
const coveredLimit = "on untested_exports_covered"

// Options says where the log is and what each line records beside the
// check's rows.
type Options struct {
	// Root is the module root, which a relative Path is resolved against
	// and the git state is read in.
	Root string
	// Module is the module path every line records.
	Module string
	// Config is the configuration the check ran with: its hook.block_log,
	// its rules' kinds and its exemptions.
	Config *config.Config
	// Version is the astimate version every line records.
	Version string
	// Now returns the time a line records and exemptions expire at; nil
	// means time.Now.
	Now func() time.Time
}

// Path returns the log file o.Config names, resolved against o.Root, or
// empty when the log is off.
func (o *Options) Path() string {
	if o.Config == nil || o.Config.Hook.BlockLog == "" {
		return ""
	}
	p := filepath.FromSlash(o.Config.Hook.BlockLog)
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(o.Root, p)
}

// Record appends to the block log what c, a hook-format check that reached
// a decision, means for it: when c failed, one block line per violation,
// without reading the log; when it passed, the log is read once and one
// pass line is appended per package and metric a block line left open that
// c judged or found deleted. It does nothing when the log is off. Its
// error never changes the check's decision; the caller warns with it.
func Record(ctx context.Context, o *Options, c *report.Check) error {
	path := o.Path()
	if path == "" {
		return nil
	}
	now := time.Now
	if o.Now != nil {
		now = o.Now
	}
	at := now()
	var lines []Line
	if c.Failed() {
		lines = blockLines(c, o.Config)
	} else {
		open, err := readOpen(path, o.Module)
		if err != nil {
			return err
		}
		lines = passLines(c, open, o.Config, at)
	}
	if len(lines) == 0 {
		return nil
	}
	commit, dirty := gitState(ctx, o.Root, path)
	var buf bytes.Buffer
	for i := range lines {
		l := &lines[i]
		l.V, l.Time, l.Commit, l.Dirty = Version, at.UTC().Format(time.RFC3339), commit, dirty
		l.Module, l.AstimateVersion = o.Module, o.Version
		if l.ConfigVersion == "" {
			l.ConfigVersion = o.Config.Version
		}
		data, err := json.Marshal(l)
		if err != nil {
			return fmt.Errorf("encoding block log line: %w", err)
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}
	return appendFile(path, buf.Bytes())
}

// rows returns c's module row, when it has one, then its packages.
func rows(c *report.Check) []*report.CheckedPackage {
	out := make([]*report.CheckedPackage, 0, len(c.Packages)+1)
	if c.Module != nil {
		out = append(out, c.Module)
	}
	for i := range c.Packages {
		out = append(out, &c.Packages[i])
	}
	return out
}

// blockLines returns one block line per violation of c, in report order.
func blockLines(c *report.Check, cfg *config.Config) []Line {
	var out []Line
	for _, p := range rows(c) {
		r := &p.Report
		for _, v := range r.Violations {
			out = append(out, Line{
				Event: EventBlock, Package: r.PackagePath, Metric: v.Metric, ConfigVersion: r.ConfigVersion,
				Block: &Block{Kind: ruleKind(cfg, r, v.Metric), Base: v.Base, Head: v.Head, Limit: v.Limit, Also: v.Also},
			})
		}
	}
	return out
}

// ruleKind returns the kind of the first rule on metric that judges r
// under r's language's configuration, as the gate picks it; empty when
// there is none.
func ruleKind(cfg *config.Config, r *report.Report, metric string) string {
	for _, t := range cfg.ForLanguage(r.Language).Thresholds {
		if t.Metric == metric && t.Judges(r.Role) {
			return string(t.Kind)
		}
	}
	return ""
}

// readOpen reads the log at path once and returns, for each package and
// metric of module whose last line is a block line, that line. A missing
// log has nothing open.
func readOpen(path, module string) (map[Key]Line, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("opening block log: %w", err)
	}
	defer func() { _ = f.Close() }()
	lines, _, err := Read(f)
	if err != nil {
		return nil, err
	}
	open := make(map[Key]Line)
	for _, l := range lines {
		if l.Module != module {
			continue
		}
		if l.Event == EventBlock {
			open[l.Key()] = l
		} else {
			delete(open, l.Key())
		}
	}
	return open, nil
}

// passLines returns a pass line for each block in open whose package c
// judged or found deleted, sorted by package and metric.
func passLines(c *report.Check, open map[Key]Line, cfg *config.Config, now time.Time) []Line {
	if len(open) == 0 {
		return nil
	}
	judged := make(map[string]*report.CheckedPackage, len(c.Packages)+1)
	for _, p := range rows(c) {
		judged[p.Report.PackagePath] = p
	}
	newPkgs := newPackages(c)
	var out []Line
	for k, b := range open {
		p, ok := judged[k.Package]
		if !ok && !slices.Contains(c.Deleted, k.Package) {
			continue
		}
		pass := &Pass{Exists: ok, NewPackages: fromBlocked(newPkgs, k.Package),
			Exempted: exempted(cfg, k.Package, append([]string{k.Metric}, b.Block.Also...), now)}
		l := Line{Event: EventPass, Package: k.Package, Metric: k.Metric, Pass: pass}
		if ok {
			l.ConfigVersion = p.Report.ConfigVersion
			pass.Value = value(&p.Report, k.Metric, b.Block.Limit)
			if k.Metric == gate.SurfaceMetric && reshapedFired(&p.Report) {
				pass.Reshaped, pass.ReshapedNames = true, p.Reshaped
			}
		}
		out = append(out, l)
	}
	slices.SortFunc(out, func(a, b Line) int {
		return cmp.Or(cmp.Compare(a.Package, b.Package), cmp.Compare(a.Metric, b.Metric))
	})
	return out
}

// newPackages returns c's packages new since the baseline, a rename's
// seeded ones included (SPEC.md 8.4), sorted by package.
func newPackages(c *report.Check) []NewPackage {
	out := []NewPackage{}
	for i := range c.Packages {
		r := &c.Packages[i].Report
		if len(r.BaselineFrom) > 0 || (r.Baseline == nil && r.BaselineUnavailable == "") {
			out = append(out, NewPackage{Package: r.PackagePath, BaselineFrom: r.BaselineFrom})
		}
	}
	slices.SortFunc(out, func(a, b NewPackage) int { return cmp.Compare(a.Package, b.Package) })
	return out
}

// fromBlocked returns a copy of pkgs with FromBlocked set on each a
// rename seeded from blocked.
func fromBlocked(pkgs []NewPackage, blocked string) []NewPackage {
	out := slices.Clone(pkgs)
	for i := range out {
		out[i].FromBlocked = slices.Contains(out[i].BaselineFrom, blocked)
	}
	return out
}

// exempted reports whether, for every metric in metrics, cfg holds an
// exemption on pkg and that metric unexpired at now: a finding merged from
// several rules moves only when each is exempted (SPEC.md 8.6).
func exempted(cfg *config.Config, pkg string, metrics []string, now time.Time) bool {
	for _, m := range metrics {
		if !slices.ContainsFunc(cfg.Exemptions, func(e exempt.Entry) bool {
			return e.Package == pkg && e.Metric == m && !e.Expired(now)
		}) {
			return false
		}
	}
	return true
}

// value returns r's value of metric, the covered count when the block
// judged untested_exports on it and the row measured it; nil when the row
// does not compute it.
func value(r *report.Report, metric, limit string) *float64 {
	name := metric
	if strings.HasSuffix(limit, coveredLimit) && r.Metrics.UntestedExportsCovered != nil {
		name = "untested_exports_covered"
	}
	v, ok := r.Metrics.Value(name)
	if !ok {
		return nil
	}
	return &v
}

// reshapedFired reports whether r carries the reshaped-exports heads-up.
func reshapedFired(r *report.Report) bool {
	return slices.ContainsFunc(r.Warnings, func(w report.Finding) bool {
		return w.Metric == gate.SurfaceMetric && gate.IsReshaped(w.Limit)
	})
}

// appendFile appends data to the file at path, creating it and its
// directory when missing, in one write.
func appendFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating block log directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening block log: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("appending to block log %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing block log %s: %w", path, err)
	}
	return nil
}
