package chat

import (
	"os"
	"path/filepath"
	"strings"
)

// ResolveStoredMediaPath turns a DB-stored file path into a path that exists on
// disk. Upload handlers store paths relative to the process cwd (e.g.
// "storage/chat/media/ab/hash.jpg"). If the API is started from another
// directory (e.g. systemd WorkingDirectory, `go run` from a subfolder), a
// naive os.Stat(dbPath) fails and media GETs return 404.
func ResolveStoredMediaPath(dbPath string) string {
	p := strings.TrimSpace(dbPath)
	if p == "" {
		return p
	}
	p = filepath.Clean(filepath.FromSlash(p))
	if filepath.IsAbs(p) {
		return p
	}
	try := func(c string) (string, bool) {
		c = filepath.Clean(c)
		if c == "" || c == "." {
			return "", false
		}
		st, err := os.Stat(c)
		if err != nil || st.IsDir() {
			return "", false
		}
		return c, true
	}
	if s, ok := try(p); ok {
		return s
	}
	if wd, err := os.Getwd(); err == nil {
		if s, ok := try(filepath.Join(wd, p)); ok {
			return s
		}
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Clean(filepath.Dir(exe))
		// Typical: bin/api.exe or cmd/api/api; walk up a few levels looking for repo root.
		for _, up := range []string{".", "..", filepath.Join("..", ".."), filepath.Join("..", "..", "..")} {
			base := filepath.Clean(filepath.Join(dir, up))
			if s, ok := try(filepath.Join(base, p)); ok {
				return s
			}
		}
	}
	// Explicit ops override: directory that contains the same layout as the DB
	// path (e.g. DB has "storage/chat/media/..." and CHAT_MEDIA_ROOT points at
	// the directory that contains "storage", or at the repo root).
	if base := strings.TrimSpace(os.Getenv("CHAT_MEDIA_ROOT")); base != "" {
		base = filepath.Clean(base)
		if s, ok := try(filepath.Join(base, p)); ok {
			return s
		}
		rel := strings.TrimPrefix(filepath.ToSlash(p), "storage/")
		if rel != p && rel != "" {
			if s, ok := try(filepath.Join(base, rel)); ok {
				return s
			}
			if s, ok := try(filepath.Join(base, "storage", rel)); ok {
				return s
			}
		}
	}
	return p
}
