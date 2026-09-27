package golang

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/rfizzle/astimate/internal/metrics"
)

// coverWaitDelay bounds how long Coverage waits for go test's output to
// close after the context stops it, since test binaries it started may
// outlive it and hold the pipes open.
const coverWaitDelay = 5 * time.Second

// Coverage measures coverage_pct for the packages pkgs, import paths of the
// module at mod.Root, with one `go test -cover -count=1 -run .` run over
// all of them in the module root (metrics.CoverageMeasurer). The
// environment, GOFLAGS and CGO_ENABLED included, is passed through
// unchanged; -coverpkg is not used, so each package's figure counts only its
// own statements. When ctx has a deadline, go test's -timeout is set to
// the time left, so a hung test binary exits with it. A package without test files or statements gets a nil
// Pct and no Reason; one whose tests fail to build or run a nil Pct and a
// Reason. When ctx ends the run, the packages it had not reported get a
// Reason saying so. The error reports a go command that could not start.
func (e *Extractor) Coverage(ctx context.Context, mod *metrics.ModuleContext, pkgs []string) (map[string]metrics.Coverage, error) {
	if len(pkgs) == 0 {
		return map[string]metrics.Coverage{}, nil
	}
	args := make([]string, 0, len(pkgs)+6)
	args = append(args, "test", "-cover", "-count=1", "-run", ".")
	if deadline, ok := ctx.Deadline(); ok {
		// Stopping the go command does not stop the test binary it runs,
		// so the binary gets the same deadline and exits on its own.
		// -timeout=0 would mean no timeout, so it is at least a second.
		left := max(time.Until(deadline).Round(time.Second), time.Second)
		args = append(args, "-timeout="+left.String())
	}
	args = append(args, pkgs...)
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = mod.Root
	cmd.WaitDelay = coverWaitDelay
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) && ctx.Err() == nil {
		return nil, fmt.Errorf("running go test in %s: %w", mod.Root, err)
	}
	stopped := ""
	if ctx.Err() != nil {
		stopped = "go test did not finish: " + ctx.Err().Error()
	}
	return parseCoverage(stdout.Bytes(), stderr.Bytes(), pkgs, stopped), nil
}

// parseCoverage reads the per-package summary lines of `go test -cover`
// from stdout and returns an entry for each of pkgs:
//
//	ok  	<pkg>	0.3s	coverage: 81.2% of statements	→ Pct 81.2
//	ok  	<pkg>	0.3s	coverage: [no statements]	→ nil, no reason
//		<pkg>		coverage: 0.0% of statements	→ nil, no test files
//	?   	<pkg>	[no test files]	→ nil, no reason
//	FAIL	<pkg> [build failed]	→ nil, the build error from stderr
//	FAIL	<pkg>	0.3s	→ nil, the first failing test
//
// A package absent from stdout gets stopped as its reason when that is
// non-empty (the run was cut short), and otherwise the first line go test
// wrote to stderr. Lines naming a package outside pkgs are ignored, so test
// output that happens to look like a summary line cannot add entries.
func parseCoverage(stdout, stderr []byte, pkgs []string, stopped string) map[string]metrics.Coverage {
	want := make(map[string]bool, len(pkgs))
	for _, p := range pkgs {
		want[p] = true
	}
	buildErrs, firstErr := buildErrors(stderr)
	out := make(map[string]metrics.Coverage, len(pkgs))
	failedTest := ""
	sc := bufio.NewScanner(bytes.NewReader(stdout))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if name, ok := failingTest(line); ok {
			if failedTest == "" {
				failedTest = name
			}
			continue
		}
		pkg, c, ok := summaryLine(line, failedTest, buildErrs, firstErr)
		if !ok {
			continue
		}
		failedTest = ""
		if want[pkg] {
			out[pkg] = c
		}
	}
	for _, p := range pkgs {
		if _, ok := out[p]; ok {
			continue
		}
		reason := stopped
		if reason == "" {
			reason = "go test reported no result"
			if firstErr != "" {
				reason += ": " + firstErr
			}
		}
		out[p] = metrics.Coverage{Reason: reason}
	}
	return out
}

// summaryLine parses one per-package summary line of go test's stdout and
// returns its package and outcome; ok is false for any other line.
// failedTest names the first test that failed since the previous summary
// line, and buildErrs and firstErr are buildErrors' results, for the reason
// of a failure.
func summaryLine(line, failedTest string, buildErrs map[string]string, firstErr string) (pkg string, c metrics.Coverage, ok bool) {
	fields := strings.Split(line, "\t")
	if len(fields) < 2 {
		return "", c, false
	}
	switch strings.TrimSpace(fields[0]) {
	case "ok":
		c.Pct = coveragePct(line)
		return fields[1], c, true
	case "?":
		return fields[1], c, true
	case "":
		// A package without test files under -cover:
		// "\t<pkg>\t\tcoverage: 0.0% of statements".
		if len(fields) >= 4 && strings.HasPrefix(fields[3], "coverage:") {
			return fields[1], c, true
		}
		return "", c, false
	case "FAIL":
		pkg, status, _ := strings.Cut(fields[1], " ")
		if status == "[build failed]" || status == "[setup failed]" {
			c.Reason = "test build failed"
			if msg := buildErrs[pkg]; msg != "" {
				c.Reason += ": " + msg
			} else if firstErr != "" {
				c.Reason += ": " + firstErr
			}
			return pkg, c, true
		}
		c.Reason = "tests failed"
		if failedTest != "" {
			c.Reason += ": " + failedTest
		}
		return pkg, c, true
	default:
		return "", c, false
	}
}

// coveragePct returns the percentage in the "coverage: NN.N% of
// statements" part of line, or nil when there is none, as for
// "coverage: [no statements]".
func coveragePct(line string) *float64 {
	_, rest, ok := strings.Cut(line, "coverage: ")
	if !ok {
		return nil
	}
	num, _, ok := strings.Cut(rest, "%")
	if !ok {
		return nil
	}
	v, err := strconv.ParseFloat(num, 64)
	if err != nil || v < 0 || v > 100 {
		return nil
	}
	return &v
}

// failingTest reports whether line is a "--- FAIL: TestName (0.00s)" line,
// at any indentation, and returns the test name.
func failingTest(line string) (string, bool) {
	rest, ok := strings.CutPrefix(strings.TrimLeft(line, " \t"), "--- FAIL: ")
	if !ok {
		return "", false
	}
	name, _, _ := strings.Cut(rest, " ")
	return name, true
}

// buildErrors reads go test's stderr, where a failed build prints a
// "# <pkg>" header (optionally followed by " [<pkg>.test]"; "# <pkg>.test"
// for a link error; "# [<pkg>]" for vet) and then its errors. It returns the first error line
// under each package's header and the first error line overall.
func buildErrors(stderr []byte) (byPkg map[string]string, first string) {
	byPkg = make(map[string]string)
	current := ""
	sc := bufio.NewScanner(bytes.NewReader(stderr))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if hdr, ok := strings.CutPrefix(line, "# "); ok {
			name, _, _ := strings.Cut(hdr, " ")
			current = strings.TrimSuffix(strings.Trim(name, "[]"), ".test")
			continue
		}
		if first == "" {
			first = line
		}
		if current != "" && byPkg[current] == "" {
			byPkg[current] = line
		}
	}
	return byPkg, first
}
