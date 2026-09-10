package main

import "testing"

func TestExplicitVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		value     string
		want      string
		wantError bool
	}{
		{name: "release tag", value: "v1.260910.0", want: "1.260910.0"},
		{name: "release candidate", value: "v1.260910.1-rc.2", want: "1.260910.1-rc.2"},
		{name: "normalized", value: "1.260910.0", want: "1.260910.0"},
		{name: "snapshot", value: "dev-abc123", want: "dev-abc123"},
		{name: "whitespace", value: "1.0 bad", wantError: true},
		{name: "linker injection", value: "1.0 -X other=value", wantError: true},
		{name: "empty normalized", value: "v", wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := readVersion(t.TempDir(), test.value)
			if (err != nil) != test.wantError {
				t.Fatalf("error = %v, wantError = %v", err, test.wantError)
			}
			if got != test.want {
				t.Fatalf("version = %q, want %q", got, test.want)
			}
		})
	}
}
