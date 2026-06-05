package httplog

import "testing"

func TestIsGitHubURL(t *testing.T) {
	tests := []struct {
		name   string
		rawURL string
		want   bool
	}{
		{name: "github api", rawURL: "https://api.github.com/repos/foo/bar/releases/latest", want: true},
		{name: "github download", rawURL: "https://github.com/foo/bar/releases/download/v1.0.0/app", want: true},
		{name: "github host uppercase", rawURL: "https://GitHub.com/foo/bar", want: true},
		{name: "non github", rawURL: "https://example.com/file", want: false},
		{name: "invalid url", rawURL: "://bad", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsGitHubURL(tt.rawURL); got != tt.want {
				t.Fatalf("IsGitHubURL(%q) = %v, want %v", tt.rawURL, got, tt.want)
			}
		})
	}
}
