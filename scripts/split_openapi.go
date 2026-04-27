package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var pathHeaderRe = regexp.MustCompile(`^  (/[^:]+):\s*$`)

var catalogOrder = []string{
	"driver",
	"dispatchers",
	"cargo_manager",
	"driver_manager",
	"admin",
	"reference",
	"chat",
	"calls",
	"company",
	"legacy_api",
	"other",
}

func writeFile(path string, lines []string) error {
	content := strings.Join(lines, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func toJSONPointer(path string) string {
	escaped := strings.ReplaceAll(path, "~", "~0")
	escaped = strings.ReplaceAll(escaped, "/", "~1")
	return escaped
}

func readLines(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	return strings.Split(strings.TrimRight(s, "\n"), "\n"), nil
}

func collectPaths(pathFile string) ([]string, error) {
	lines, err := readLines(pathFile)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0)
	for _, line := range lines {
		m := pathHeaderRe.FindStringSubmatch(line)
		if m != nil {
			paths = append(paths, m[1])
		}
	}
	return paths, nil
}

func includePathInRoot(path string) bool {
	p := strings.ToLower(strings.TrimSpace(path))
	if strings.Contains(p, "driver-invitations") {
		return false
	}
	if strings.Contains(p, "dispatcher-invitations") {
		return false
	}
	return true
}

func main() {
	root, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	baseFile := filepath.Join(root, "docs", "openapi", "base.yaml")
	pathsDir := filepath.Join(root, "docs", "openapi", "paths")
	unifiedTarget := filepath.Join(root, "docs", "openapi", "root.yaml")

	baseLines, err := readLines(baseFile)
	if err != nil {
		panic(fmt.Errorf("base file read failed: %w", err))
	}
	if len(baseLines) == 0 {
		panic("base file is empty")
	}

	out := append([]string{}, baseLines...)
	out = append(out, "paths:")

	for _, catalog := range catalogOrder {
		pathFile := filepath.Join(pathsDir, catalog+".yaml")
		if _, err := os.Stat(pathFile); err != nil {
			continue
		}
		paths, err := collectPaths(pathFile)
		if err != nil {
			panic(fmt.Errorf("failed to parse %s: %w", pathFile, err))
		}
		if len(paths) == 0 {
			continue
		}
		out = append(out, "  # "+catalog)
		for _, p := range paths {
			if !includePathInRoot(p) {
				continue
			}
			out = append(out, "  "+p+":")
			out = append(out, "    $ref: './paths/"+catalog+".yaml#/paths/"+toJSONPointer(p)+"'")
		}
	}

	if err := writeFile(unifiedTarget, out); err != nil {
		panic(err)
	}
	fmt.Println("OpenAPI root rebuilt from split files")
}
