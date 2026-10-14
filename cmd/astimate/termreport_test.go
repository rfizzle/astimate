package main

import (
	"bytes"
	"context"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/engine"
	"github.com/rfizzle/astimate/internal/narrate"
	"github.com/rfizzle/astimate/internal/narrate/render"
)

// termLayouts are the widths and colours the text goldens are written at:
// 120 columns shows every column and stacks nothing, 80 drops the copied
// and untested columns, each with and without colour.
func termLayouts() []render.Options {
	return []render.Options{{Width: 120}, {Width: 120, Color: true}, {Width: 80}, {Width: 80, Color: true}}
}

// termGolden is the golden file of the text report name at opts.
func termGolden(name string, opts render.Options) string {
	name += "_" + strconv.Itoa(opts.Width)
	if opts.Color {
		name += "_color"
	}
	return name + ".txt"
}

// checkTextGoldens renders m as text at every termLayouts layout, with
// base on top of each, against its golden, and returns the text at the
// default width without colour, which is what the command prints to a
// pipe.
func checkTextGoldens(t *testing.T, name string, m *narrate.Module, base render.Options) string {
	t.Helper()
	for _, opts := range termLayouts() {
		opts.Exit, opts.ChangeOnly = base.Exit, base.ChangeOnly
		var buf bytes.Buffer
		if err := render.Write(t.Context(), &buf, m, render.FormatText, &opts); err != nil {
			t.Fatal(err)
		}
		got := buf.String()
		if hasEscape := strings.Contains(got, "\x1b["); hasEscape != opts.Color {
			t.Errorf("%s: escape codes %v, want %v", termGolden(name, opts), hasEscape, opts.Color)
		}
		compareGolden(t, termGolden(name, opts), got)
	}
	base.Width = 100
	var buf bytes.Buffer
	if err := render.Write(t.Context(), &buf, m, render.FormatText, &base); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// TestReportTextGolden checks the text report of the fixture against
// testdata/report_fixture_<width>[_color].txt (regenerate with
// UPDATE_GOLDEN=1), and that report prints it to a pipe at 100 columns
// without colour, and with colour under --color always.
func TestReportTextGolden(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	_, fixture := reportFixture(t)
	cfg := pinnedConfigFile(t)
	tg, err := loadTarget(fixture, cfg, tokenizerEst, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	m, failed, err := engine.Report(context.Background(), tg, engine.ReportOptions{})
	if err != nil || len(failed) > 0 {
		t.Fatalf("Report = %v, %v", failed, err)
	}
	m.AstimateVersion = "test"
	want := checkTextGoldens(t, "report_fixture", m, render.Options{})

	var stdout, stderr bytes.Buffer
	args := []string{"report", fixture, "--config", cfg}
	if got := run(args, &stdout, &stderr); got != exitOK || stdout.String() != want {
		t.Errorf("run(%q) = %d with stdout\n%s\nstderr %q; want %d and the text at 100 columns:\n%s",
			args, got, stdout.String(), stderr.String(), exitOK, want)
	}
	stdout.Reset()
	if got := run(append(args, "--color", "always"), &stdout, &stderr); got != exitOK || !strings.Contains(stdout.String(), "\x1b[") {
		t.Errorf("--color always = %d with stdout\n%s\nwant colour", got, stdout.String())
	}
}

// TestCheckTextGolden checks check --base on the degraded fixture, the
// pristine fixture committed and package tested degraded in the working
// tree, against testdata/check_degraded_<width>[_color].txt, and that
// check prints it to a pipe at 100 columns without colour and exits 3.
func TestCheckTextGolden(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages and runs git")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Parallel()

	repo, fixture := reportFixture(t)
	gitIn(t, repo, "init", "-q", "-b", "master")
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-q", "--no-verify", "-m", "pristine fixture")
	copyTree(t, filepath.Join(degradedDir, "tested"), filepath.Join(fixture, "tested"))
	cfg := pinnedConfigFile(t)
	tg, err := loadTarget(fixture, cfg, tokenizerEst, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	c, failed, err := engine.Check(context.Background(), tg, engine.CheckOptions{Base: "master"})
	if err != nil || len(failed) > 0 {
		t.Fatalf("Check = %v, %v", failed, err)
	}
	exit := exitGateFailed
	want := checkTextGoldens(t, "check_degraded", engine.CheckReport(tg, c, "master"),
		render.Options{Exit: &exit, ChangeOnly: true})

	var stdout, stderr bytes.Buffer
	args := []string{"check", fixture, "--base", "master", "--config", cfg}
	if got := run(args, &stdout, &stderr); got != exitGateFailed || stdout.String() != want {
		t.Errorf("run(%q) = %d with stdout\n%s\nstderr %q; want %d and the text at 100 columns:\n%s",
			args, got, stdout.String(), stderr.String(), exitGateFailed, want)
	}
}
