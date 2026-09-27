package main

import (
	"fmt"
	"io"
	"runtime/debug"
)

// unknownBuildValue is printed for a build field neither -ldflags nor the
// runtime build info supplies.
const unknownBuildValue = "unknown"

// buildMeta is the version, commit and build date reported by `version`.
type buildMeta struct {
	version string
	commit  string
	date    string
}

// runVersion prints the version, commit and build date to stdout.
func runVersion(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		_, _ = fmt.Fprintf(stderr, "astimate: version takes no arguments\n\n%s", usage)
		return exitUsage
	}
	info, ok := debug.ReadBuildInfo()
	writeVersion(stdout, resolveBuildMeta(buildMeta{buildVersion, buildCommit, buildDate}, info, ok))
	return exitOK
}

// resolveBuildMeta fills each empty field of injected from the runtime build
// info, then from unknownBuildValue, so `go run` and `go install` builds still
// report something sensible.
func resolveBuildMeta(injected buildMeta, info *debug.BuildInfo, ok bool) buildMeta {
	var fallback buildMeta
	if ok && info != nil {
		fallback.version = info.Main.Version
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				fallback.commit = s.Value
			case "vcs.time":
				fallback.date = s.Value
			}
		}
	}
	return buildMeta{
		version: firstNonEmpty(injected.version, fallback.version),
		commit:  firstNonEmpty(injected.commit, fallback.commit),
		date:    firstNonEmpty(injected.date, fallback.date),
	}
}

func firstNonEmpty(a, b string) string {
	switch {
	case a != "":
		return a
	case b != "":
		return b
	default:
		return unknownBuildValue
	}
}

func writeVersion(w io.Writer, m buildMeta) {
	_, _ = fmt.Fprintf(w, "version: %s\ncommit: %s\ndate: %s\n", m.version, m.commit, m.date)
}
