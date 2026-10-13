package rec

import "testing"

func TestNew(t *testing.T) {
	var r Recorder = New()
	if r == nil {
		t.Fatal("no recorder")
	}
}
