package config

import (
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

// LoadDotEnvUp loads the first ".env" found in this order:
//  1) ./.env in the current working directory (so the project repo wins);
//  2) .env in each parent directory, up to maxDepth levels above cwd;
//  3) default godotenv.Load() (current dir only, library default).
//
// Previously the search started from the parent of cwd, so a stray .env
// in a parent folder (e.g. Desktop/.env) could be loaded instead of the project's .env.
func LoadDotEnvUp(maxDepth int) {
	if maxDepth <= 0 {
		maxDepth = 6
	}

	cwd, err := os.Getwd()
	if err != nil {
		_ = godotenv.Load()
		return
	}

	if tryLoadDotEnv(filepath.Join(cwd, ".env")) {
		return
	}

	dir := cwd
	for i := 0; i <= maxDepth; i++ {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
		if tryLoadDotEnv(filepath.Join(dir, ".env")) {
			return
		}
	}

	_ = godotenv.Load()
}

func tryLoadDotEnv(p string) bool {
	if _, err := os.Stat(p); err != nil {
		return false
	}
	if err := godotenv.Load(p); err != nil {
		return false
	}
	return true
}
