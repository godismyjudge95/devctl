package install

import (
	"os"
	"path/filepath"
	"runtime"
)

// pgShlibExt is .dylib on Darwin and .so on Linux.
func pgShlibExt() string {
	if runtime.GOOS == "darwin" {
		return ".dylib"
	}
	return ".so"
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// pgLibDir is the PostgreSQL pkglibdir. EDB Darwin uses lib/postgresql.
func pgLibDir(pgDir string) string {
	for _, d := range []string{
		filepath.Join(pgDir, "lib", "postgresql"),
		filepath.Join(pgDir, "lib"),
	} {
		if dirExists(d) {
			return d
		}
	}
	return filepath.Join(pgDir, "lib")
}

// pgShareExtDir is the extension control/SQL directory.
func pgShareExtDir(pgDir string) string {
	for _, d := range []string{
		filepath.Join(pgDir, "share", "postgresql", "extension"),
		filepath.Join(pgDir, "share", "extension"),
	} {
		if dirExists(d) {
			return d
		}
	}
	return filepath.Join(pgDir, "share", "extension")
}

func pgShlibPath(pgDir, stem string) string {
	return filepath.Join(pgLibDir(pgDir), stem+pgShlibExt())
}

func pgControlPath(pgDir, id string) string {
	return filepath.Join(pgShareExtDir(pgDir), id+".control")
}
