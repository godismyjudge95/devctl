package dist

import (
	"bytes"
	"testing"
)

func TestLooksLikeNativeMagic(t *testing.T) {
	cases := []struct {
		name  string
		magic [4]byte
		want  bool
	}{
		{"elf", [4]byte{0x7f, 'E', 'L', 'F'}, true},
		{"macho64le", [4]byte{0xcf, 0xfa, 0xed, 0xfe}, true},
		{"macho64be", [4]byte{0xfe, 0xed, 0xfa, 0xcf}, true},
		{"macho32le", [4]byte{0xce, 0xfa, 0xed, 0xfe}, true},
		{"fat", [4]byte{0xca, 0xfe, 0xba, 0xbe}, true},
		{"shell", [4]byte{'#', '!', '/', 'b'}, false},
		{"empty", [4]byte{0, 0, 0, 0}, false},
	}
	for _, tc := range cases {
		if got := LooksLikeNativeMagic(tc.magic); got != tc.want {
			t.Errorf("%s: LooksLikeNativeMagic(%x) = %v, want %v", tc.name, tc.magic, got, tc.want)
		}
		if got := LooksLikeNative(bytes.NewReader(tc.magic[:])); got != tc.want {
			t.Errorf("%s: LooksLikeNative = %v, want %v", tc.name, got, tc.want)
		}
	}
}
