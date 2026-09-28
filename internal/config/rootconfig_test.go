package config

import (
	"path/filepath"
	"reflect"
	"testing"
)

// TestRootConfigMatchesDefault checks that this repository's astimate.yaml,
// which the self-check resolves from the repository root, is the embedded
// default in everything but its exemptions, so a change to default.yaml
// cannot silently leave the repository gating itself by stale rules.
func TestRootConfigMatchesDefault(t *testing.T) {
	root, err := Load(filepath.Join("..", "..", FileName))
	if err != nil {
		t.Fatalf("loading the repository's %s: %v", FileName, err)
	}
	def, err := Parse(Default())
	if err != nil {
		t.Fatalf("parsing the embedded default: %v", err)
	}
	if len(root.Exemptions) == 0 {
		t.Error("the repository's astimate.yaml records no exemptions; delete it and use the embedded default instead")
	}
	got := *root
	got.Exemptions = def.Exemptions
	if !reflect.DeepEqual(&got, def) {
		t.Errorf("the repository's %s differs from internal/config/default.yaml outside exemptions; "+
			"regenerate it from default.yaml and re-append its exemptions section", FileName)
	}
}
