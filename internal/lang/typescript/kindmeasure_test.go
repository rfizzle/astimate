package typescript

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

// TestMeasureBlockKinds measures the duplicate block kinds of SPEC.md 6.3
// over the TypeScript module in $ASTIMATE_DUPKINDS_DIR and writes every
// package's dup_blocks, dup_blocks_code and blocks with their kinds as
// JSON to $ASTIMATE_DUPKINDS_OUT, in the layout of the Go extractor's
// measurement (cross is always null), for
// calibration/notes/dup-block-kinds-2026-10-14.md. It runs only when both
// are set.
func TestMeasureBlockKinds(t *testing.T) {
	dir, out := os.Getenv("ASTIMATE_DUPKINDS_DIR"), os.Getenv("ASTIMATE_DUPKINDS_OUT")
	if dir == "" || out == "" {
		t.Skip("set ASTIMATE_DUPKINDS_DIR and ASTIMATE_DUPKINDS_OUT to measure")
	}
	type block struct {
		Kind      string    `json:"kind"`
		Anchors   [2]uint64 `json:"anchors"`
		Locations []string  `json:"locations"`
	}
	type pkg struct {
		Blocks int     `json:"dup_blocks"`
		Code   int     `json:"dup_blocks_code"`
		List   []block `json:"blocks"`
	}
	res := struct {
		Module   string         `json:"module"`
		Packages map[string]pkg `json:"packages"`
		Cross    []block        `json:"cross"`
	}{Module: dir, Packages: make(map[string]pkg)}
	e := New()
	pkgs, err := e.Packages(dir)
	if err != nil {
		t.Fatal(err)
	}
	mod := &metrics.ModuleContext{Root: dir}
	for _, id := range pkgs {
		m, err := e.Extract(t.Context(), mod, id)
		if err != nil {
			t.Fatal(err)
		}
		det, err := e.Details(t.Context(), mod, id)
		if err != nil {
			t.Fatal(err)
		}
		row := pkg{Blocks: m.DupBlocks, Code: *m.DupBlocksCode}
		for _, b := range det.DupBlocks {
			row.List = append(row.List, block{Kind: b.Kind, Anchors: b.Anchors, Locations: b.Locations})
		}
		res.Packages[id] = row
	}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
