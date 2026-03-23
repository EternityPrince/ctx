package text

import "testing"

func TestNormalizeNewlines(t *testing.T) {
	got := NormalizeNewlines("alpha\r\nbeta\r\ngamma\n")
	want := "alpha\nbeta\ngamma\n"
	if got != want {
		t.Fatalf("unexpected normalized text: got %q want %q", got, want)
	}
}

func TestCountLines(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		wantTotal    int
		wantNonEmpty int
	}{
		{
			name:         "empty",
			content:      "",
			wantTotal:    0,
			wantNonEmpty: 0,
		},
		{
			name:         "trailing newline is not extra line",
			content:      "alpha\n\n beta \n",
			wantTotal:    3,
			wantNonEmpty: 2,
		},
		{
			name:         "whitespace lines are empty",
			content:      " \n\t\n",
			wantTotal:    2,
			wantNonEmpty: 0,
		},
		{
			name:         "single line without newline",
			content:      "value",
			wantTotal:    1,
			wantNonEmpty: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTotal, gotNonEmpty := CountLines(tt.content)
			if gotTotal != tt.wantTotal || gotNonEmpty != tt.wantNonEmpty {
				t.Fatalf("unexpected counts for %q: got (%d, %d) want (%d, %d)", tt.content, gotTotal, gotNonEmpty, tt.wantTotal, tt.wantNonEmpty)
			}
		})
	}
}
