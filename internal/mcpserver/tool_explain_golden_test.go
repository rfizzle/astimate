package mcpserver

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

// updateFlag names the flag that makes TestExplainMetricGolden rewrite its
// golden file under testdata/explain. Use it only after an intentional
// change to a metric's explanation, then read the result.
const updateFlag = "update"

// TestMain defines the -update flag before the test binary parses flags;
// the golden test reads it back with flag.Lookup, so no package variable
// holds it.
func TestMain(m *testing.M) {
	flag.Bool(updateFlag, false, "rewrite the mcpserver golden files")
	flag.Parse()
	os.Exit(m.Run())
}

// TestExplainMetricGolden calls explain_metric for every metric with no
// loaded configuration, so the output depends on the explanation texts
// alone, and compares the text and structured content byte for byte with
// testdata/explain/all.golden.
func TestExplainMetricGolden(t *testing.T) {
	t.Parallel()

	cs := newTestClient(t, Options{})
	var b bytes.Buffer
	for _, name := range metrics.MetricNames() {
		res := callExplain(t, cs, name)
		if res.IsError {
			t.Fatalf("explain_metric %s: IsError = true; text:\n%s", name, resultText(res))
		}
		data, err := json.MarshalIndent(res.StructuredContent, "", "  ")
		if err != nil {
			t.Fatalf("encoding structured content of %s: %v", name, err)
		}
		b.WriteString("== " + name + " ==\n" + resultText(res) + "-- structured --\n")
		b.Write(data)
		b.WriteString("\n\n")
	}
	path := filepath.Join("testdata", "explain", "all.golden")
	if flag.Lookup(updateFlag).Value.String() == "true" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden: %v (run with -update to create it)", err)
	}
	if !bytes.Equal(b.Bytes(), want) {
		t.Errorf("explain_metric output differs from %s; rerun with -update only after an intentional text change\ngot:\n%s", path, b.String())
	}
}
