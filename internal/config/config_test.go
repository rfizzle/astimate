package config

import (
	"bytes"
	"testing"
)

func TestDefault(t *testing.T) {
	t.Parallel()

	got := Default()
	for _, key := range []string{"config_version:", "chars_per_token:", "dup_min_tokens:", "weights:", "thresholds:"} {
		if !bytes.Contains(got, []byte(key)) {
			t.Errorf("Default() missing %q", key)
		}
	}

	got[0] = 'X'
	if Default()[0] == 'X' {
		t.Error("Default() returned shared storage; mutation leaked")
	}
}
