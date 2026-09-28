package main

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/model"
	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/synth"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/runner"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/score"
)

// baseConfig is the fit's --base: the pre-calibration placeholders, so every
// fitted parameter moves off a known value whatever the shipped default holds.
const baseConfig = "../../../configs/uncalibrated.yaml"

// plant is the synthetic experiment the end-to-end tests fit: 60 packages,
// three runs each, 3% noise, every seventh run failing.
func plant() *synth.Plant {
	return &synth.Plant{
		Params: model.Params{Overhead: 12000, Scale: 3, Budget: 25000,
			PerExport: 60, PerUntested: 500, PerHidden: 300, Exponent: 1.4},
		Noise: 0.03, Runs: 3, FailEvery: 7, Agent: "synthetic", Model: "synthetic-model", Seed: 11,
	}
}

// writeRuns writes rows as a runs.jsonl in a temporary directory.
func writeRuns(t *testing.T, rows []runner.RunRow) string {
	t.Helper()
	var b bytes.Buffer
	for _, r := range rows {
		line, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	p := filepath.Join(t.TempDir(), "runs.jsonl")
	if err := os.WriteFile(p, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// fitSynthetic runs the command on the planted rows and returns the
// candidate's and the report's paths.
func fitSynthetic(t *testing.T) (out, rep string) {
	t.Helper()
	runs := writeRuns(t, synth.Rows(synth.RandomMetrics(60, 21), plant()))
	dir := t.TempDir()
	out, rep = filepath.Join(dir, "candidate.yaml"), filepath.Join(dir, "report.md")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--runs", runs, "--base", baseConfig, "--date", "2026-09-28",
		"--out", out, "--report", rep}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr: %s", stderr.String())
	}
	if want := "rebuild-2026-09-28-synthetic"; !strings.Contains(stdout.String(), want) {
		t.Errorf("stdout %q does not name %s", stdout.String(), want)
	}
	return out, rep
}

func TestFitRecoversPlantedParameters(t *testing.T) {
	out, _ := fitSynthetic(t)
	cfg, err := config.Load(out)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != "rebuild-2026-09-28-synthetic" || !score.Calibrated(cfg.Version) {
		t.Errorf("config_version %q, want a calibrated rebuild-2026-09-28-synthetic", cfg.Version)
	}
	if err := cfg.Rebuild.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
	p := plant().Params
	for _, c := range []struct {
		name      string
		got, want float64
		tol       float64
	}{
		{"context_budget", cfg.Rebuild.ContextBudget, p.Budget, 0},
		{"tokens_per_export", cfg.Rebuild.TokensPerExport, p.PerExport, 0.2},
		{"tokens_per_untested_export", cfg.Rebuild.TokensPerUntestedExport, p.PerUntested, 0.1},
		{"tokens_per_hidden_state", cfg.Rebuild.TokensPerHiddenState, p.PerHidden, 0.25},
		{"superlinear_exponent", cfg.Rebuild.SuperlinearExponent, p.Exponent, 0.03},
	} {
		if math.Abs(c.got-c.want) > c.tol*c.want {
			t.Errorf("%s = %v, want %v within %.0f%%", c.name, c.got, c.want, c.tol*100)
		}
	}

	// Everything but the rebuild parameters is the base, byte for byte.
	base, err := os.ReadFile(baseConfig)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	bl, gl := strings.Split(string(base), "\n"), strings.Split(string(got), "\n")
	if len(bl) != len(gl) {
		t.Fatalf("%d lines, base has %d", len(gl), len(bl))
	}
	changed := map[string]bool{}
	for i := range bl {
		if bl[i] != gl[i] {
			key, _, _ := strings.Cut(strings.TrimSpace(gl[i]), ":")
			changed[key] = true
		}
	}
	for k := range changed {
		switch k {
		case "config_version", "tokens_per_export", "tokens_per_untested_export", "tokens_per_hidden_state", "superlinear_exponent":
		default:
			t.Errorf("line %q changed", k)
		}
	}
	if len(cfg.Thresholds) == 0 {
		t.Error("thresholds lost")
	}
}

func TestReportNamesCensoredRunsAndSilentInputs(t *testing.T) {
	_, rep := fitSynthetic(t)
	data, err := os.ReadFile(rep)
	if err != nil {
		t.Fatal(err)
	}
	// Paragraphs are wrapped; compare with every run of white space as
	// one space.
	r := strings.Join(strings.Fields(string(data)), " ")
	// 180 runs, every seventh failing: 25 censored.
	for _, want := range []string{
		"# Rebuild parameters rebuild-2026-09-28-synthetic",
		"180 runs of 60 packages by synthetic (model synthetic-model)",
		"25 runs failed and are censored",
		"## Censored runs",
		"hit the turn cap of 100",
		"## Fit quality per output",
		"### Residuals by tier",
		"| Measured tokens | 60 | the 7.2 form |",
		"| Turns (median of passes) |",
		"No measurable contribution, to tokens or to passing:",
		"| `max_nesting` |",
		"`tokens_per_export` | 40 |",
		"ASTIMATE_CONFIG=" + filepath.ToSlash(filepath.Dir(rep)),
	} {
		if !strings.Contains(r, want) {
			t.Errorf("report lacks %q", want)
		}
	}
	if n := strings.Count(r, "tests fail"); n != 25 {
		t.Errorf("%d censored rows in the report, want 25", n)
	}
	// The planted costs do not read the complexity candidates, and the
	// volume term is planted: the report must not call it silent.
	_, sentence, _ := strings.Cut(r, "No measurable contribution, to tokens or to passing:")
	sentence, _, _ = strings.Cut(sentence, ".")
	if strings.Contains(sentence, "volume") || strings.Contains(sentence, model.InUntested) {
		t.Errorf("planted inputs named silent: %s", sentence)
	}
	if !strings.Contains(sentence, model.InMaxNesting) && !strings.Contains(sentence, model.InCognitiveP90) {
		t.Errorf("no unplanted complexity input named silent: %s", sentence)
	}
}

func TestEmittedConfigPassesInvariants(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the invariants suite in a child go test")
	}
	out, _ := fitSynthetic(t)
	abs, err := filepath.Abs(out)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "go", "test", "-count=1", "./internal/invariants")
	cmd.Dir = "../../.."
	cmd.Env = append(os.Environ(), "ASTIMATE_CONFIG="+abs)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("invariants under the fitted config: %v\n%s", err, b)
	}
}

func TestDryRunRow(t *testing.T) {
	dir := t.TempDir()
	out, rep := filepath.Join(dir, "c.yaml"), filepath.Join(dir, "r.md")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--runs", "testdata/dryrun-runs.jsonl", "--agent", "dry-run", "--date", "2026-09-28",
		"--out", out, "--report", rep}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "no parameter could be fitted") {
		t.Errorf("stderr %q does not warn that nothing was fitted", stderr.String())
	}
	cfg, err := config.Load(out)
	if err != nil {
		t.Fatal(err)
	}
	def, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Rebuild != def.Rebuild {
		t.Errorf("rebuild %+v, want the base's %+v unchanged", cfg.Rebuild, def.Rebuild)
	}
	if cfg.Version != "rebuild-2026-09-28-dry-run" {
		t.Errorf("config_version %q", cfg.Version)
	}
	r, err := os.ReadFile(rep)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"could not be fitted", "must not ship", "1 packages have a passing run"} {
		if !strings.Contains(string(r), want) {
			t.Errorf("report lacks %q", want)
		}
	}
}

func TestRunErrors(t *testing.T) {
	rows := synth.Rows(synth.RandomMetrics(3, 1), plant())
	mixed := rows[:2]
	mixed[1].Agent.Name = "other"
	mixedPath := writeRuns(t, mixed)
	unnamed := synth.Rows(synth.RandomMetrics(3, 1), plant())
	for i := range unnamed {
		unnamed[i].Agent.Name = "Has Space"
	}
	unnamedPath := writeRuns(t, unnamed)
	tests := []struct {
		name string
		args []string
		code int
		want string
	}{
		{"no runs", nil, 2, "--runs is required"},
		{"bad date", []string{"--runs", "x", "--date", "28-09-2026"}, 2, "--date"},
		{"bad measure", []string{"--runs", "x", "--measure", "cost"}, 2, "unknown token measure"},
		{"bad agent", []string{"--runs", "x", "--agent", "A B"}, 2, "--agent"},
		{"negative budget", []string{"--runs", "x", "--budget", "-1"}, 2, "--budget"},
		{"extra argument", []string{"--runs", "x", "extra"}, 2, "unexpected arguments"},
		{"missing file", []string{"--runs", filepath.Join(t.TempDir(), "none.jsonl")}, 1, "reading runs"},
		{"mixed agents", []string{"--runs", mixedPath}, 1, "fit one agent at a time"},
		{"unusable agent name", []string{"--runs", unnamedPath}, 1, "pass --agent"},
		{"missing base", []string{"--runs", unnamedPath, "--agent", "a", "--base", "none.yaml"}, 1, "base config"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(tt.args, &stdout, &stderr); code != tt.code {
				t.Fatalf("exit %d, want %d: %s", code, tt.code, stderr.String())
			}
			if !strings.Contains(stderr.String(), tt.want) {
				t.Errorf("stderr %q lacks %q", stderr.String(), tt.want)
			}
		})
	}
}

func TestUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--nope"}, &stdout, &stderr); code != 2 {
		t.Errorf("unknown flag: exit %d, want 2", code)
	}
	// The flag package reported it; run adds nothing.
	if n := strings.Count(stderr.String(), "flag provided but not defined"); n != 1 {
		t.Errorf("stderr reports the unknown flag %d times: %s", n, stderr.String())
	}
	u := &usageError{err: os.ErrInvalid}
	if u.Error() != os.ErrInvalid.Error() {
		t.Errorf("Error = %q", u.Error())
	}
}

func TestRunsFlag(t *testing.T) {
	var r runsFlag
	for _, v := range []string{"a.jsonl", "b.jsonl"} {
		if err := r.Set(v); err != nil {
			t.Fatal(err)
		}
	}
	if got := r.String(); got != "a.jsonl,b.jsonl" {
		t.Errorf("String = %q", got)
	}
}

func TestCommand(t *testing.T) {
	got := command([]string{"--runs", "a b.jsonl", "--date", "2026-09-28"})
	if want := "go run ./calibration/rebuild/fit --runs 'a b.jsonl' --date 2026-09-28"; got != want {
		t.Errorf("command = %q, want %q", got, want)
	}
}
