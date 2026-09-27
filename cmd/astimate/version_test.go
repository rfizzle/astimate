package main

import (
	"bytes"
	"runtime/debug"
	"strings"
	"testing"
)

func TestResolveBuildMeta(t *testing.T) {
	t.Parallel()

	vcsInfo := &debug.BuildInfo{
		Main: debug.Module{Version: "(devel)"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "deadbeef"},
			{Key: "vcs.time", Value: "2026-09-01T12:00:00Z"},
		},
	}

	tests := []struct {
		name     string
		injected buildMeta
		info     *debug.BuildInfo
		ok       bool
		want     buildMeta
	}{
		{
			name:     "ldflags win",
			injected: buildMeta{version: "v1.2.3", commit: "abc123", date: "2026-01-02T03:04:05Z"},
			info:     vcsInfo,
			ok:       true,
			want:     buildMeta{version: "v1.2.3", commit: "abc123", date: "2026-01-02T03:04:05Z"},
		},
		{
			name: "build info fallback",
			info: vcsInfo,
			ok:   true,
			want: buildMeta{version: "(devel)", commit: "deadbeef", date: "2026-09-01T12:00:00Z"},
		},
		{
			name: "nothing available",
			want: buildMeta{version: unknownBuildValue, commit: unknownBuildValue, date: unknownBuildValue},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveBuildMeta(tt.injected, tt.info, tt.ok); got != tt.want {
				t.Errorf("resolveBuildMeta() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestWriteVersion(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writeVersion(&buf, buildMeta{version: "v1.2.3", commit: "abc123", date: "2026-01-02T03:04:05Z"})

	want := "version: v1.2.3\ncommit: abc123\ndate: 2026-01-02T03:04:05Z\n"
	if got := buf.String(); got != want {
		t.Errorf("writeVersion() = %q, want %q", got, want)
	}
}

func TestRunVersion(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	if got := run([]string{"version"}, &stdout, &stderr); got != exitOK {
		t.Fatalf("run(version) exit code = %d, want %d; stderr = %q", got, exitOK, stderr.String())
	}

	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	prefixes := []string{"version: ", "commit: ", "date: "}
	if len(lines) != len(prefixes) {
		t.Fatalf("run(version) stdout = %q, want %d lines", stdout.String(), len(prefixes))
	}
	for i, p := range prefixes {
		if !strings.HasPrefix(lines[i], p) || len(lines[i]) == len(p) {
			t.Errorf("run(version) line %d = %q, want non-empty %q field", i, lines[i], p)
		}
	}
	if stderr.Len() != 0 {
		t.Errorf("run(version) stderr = %q, want empty", stderr.String())
	}
}

func TestRunVersionRejectsArgs(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	if got := run([]string{"version", "extra"}, &stdout, &stderr); got != exitUsage {
		t.Errorf("run(version extra) exit code = %d, want %d", got, exitUsage)
	}
}
