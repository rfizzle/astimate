package diff

import (
	"reflect"
	"testing"
)

func TestParseRenames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		out     string
		want    [][2]string
		wantErr bool
	}{
		{name: "empty"},
		{
			name: "mixed",
			out:  "M\x00a/a.go\x00R097\x00a/x.go\x00a/sub/x.go\x00C100\x00b/b.go\x00c/b.go\x00D\x00d/d.go\x00",
			want: [][2]string{{"a/x.go", "a/sub/x.go"}, {"b/b.go", "c/b.go"}},
		},
		{name: "truncated rename", out: "R100\x00a/x.go\x00", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseRenames(tt.out)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseRenames error = %v, want error %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseRenames = %q, want %q", got, tt.want)
			}
		})
	}
}
