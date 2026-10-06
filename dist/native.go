package dist

import "io"

// LooksLikeNative reports whether r starts with ELF or Mach-O magic.
func LooksLikeNative(r io.Reader) bool {
	var magic [4]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil {
		return false
	}
	return LooksLikeNativeMagic(magic)
}

// LooksLikeNativeMagic reports whether magic is ELF or Mach-O (including fat).
func LooksLikeNativeMagic(magic [4]byte) bool {
	if magic[0] == 0x7f && magic[1] == 'E' && magic[2] == 'L' && magic[3] == 'F' {
		return true
	}
	be := uint32(magic[0])<<24 | uint32(magic[1])<<16 | uint32(magic[2])<<8 | uint32(magic[3])
	switch be {
	case 0xfeedface, 0xcefaedfe, 0xfeedfacf, 0xcffaedfe, 0xcafebabe, 0xbebafeca:
		return true
	}
	return false
}
