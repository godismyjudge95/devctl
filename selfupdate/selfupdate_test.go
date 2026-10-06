package selfupdate

import "testing"

func TestReleaseAssetName(t *testing.T) {
	cases := []struct {
		goos, want string
	}{
		{"linux", "devctl-linux-x86_64"},
		{"darwin", "devctl-macos-aarch64"},
	}
	for _, tc := range cases {
		got := releaseAssetName(tc.goos)
		if got != tc.want {
			t.Errorf("releaseAssetName(%q) = %q, want %q", tc.goos, got, tc.want)
		}
	}
}
