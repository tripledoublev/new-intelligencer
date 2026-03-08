package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func loadDotEnv() error {
	path, err := findDotEnv(defaultDotEnvSearchRoots())
	if err != nil {
		return err
	}
	if path == "" {
		return nil
	}
	return applyDotEnvFile(path)
}

func defaultDotEnvSearchRoots() []string {
	roots := []string{}

	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		roots = append(roots, cwd)
	}

	if exePath, err := os.Executable(); err == nil && exePath != "" {
		roots = append(roots, filepath.Dir(exePath))
	}

	return roots
}

func findDotEnv(startDirs []string) (string, error) {
	seen := make(map[string]bool)

	for _, startDir := range startDirs {
		if startDir == "" {
			continue
		}

		dir, err := filepath.Abs(startDir)
		if err != nil {
			return "", fmt.Errorf("resolving dotenv search path %q: %w", startDir, err)
		}

		for {
			if seen[dir] {
				break
			}
			seen[dir] = true

			candidate := filepath.Join(dir, ".env")
			info, err := os.Stat(candidate)
			if err == nil {
				if info.IsDir() {
					return "", fmt.Errorf("dotenv path is a directory: %s", candidate)
				}
				return candidate, nil
			}
			if !errors.Is(err, os.ErrNotExist) {
				return "", fmt.Errorf("checking dotenv file %s: %w", candidate, err)
			}

			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	return "", nil
}

func applyDotEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening dotenv file %s: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		key, value, err := parseDotEnvLine(line)
		if err != nil {
			return fmt.Errorf("parsing %s:%d: %w", path, lineNo, err)
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("setting %s from %s:%d: %w", key, path, lineNo, err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading dotenv file %s: %w", path, err)
	}

	return nil
}

func parseDotEnvLine(line string) (string, string, error) {
	idx := strings.IndexRune(line, '=')
	if idx <= 0 {
		return "", "", fmt.Errorf("expected KEY=VALUE")
	}

	key := strings.TrimSpace(line[:idx])
	if key == "" {
		return "", "", fmt.Errorf("missing key")
	}

	value, err := parseDotEnvValue(strings.TrimSpace(line[idx+1:]))
	if err != nil {
		return "", "", err
	}

	return key, value, nil
}

func parseDotEnvValue(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}

	switch raw[0] {
	case '"':
		end := findClosingDoubleQuote(raw)
		if end < 0 {
			return "", fmt.Errorf("unterminated double-quoted value")
		}

		decoded, err := strconv.Unquote(raw[:end+1])
		if err != nil {
			return "", fmt.Errorf("invalid double-quoted value: %w", err)
		}

		if err := validateDotEnvTail(raw[end+1:]); err != nil {
			return "", err
		}

		return decoded, nil
	case '\'':
		end := strings.IndexByte(raw[1:], '\'')
		if end < 0 {
			return "", fmt.Errorf("unterminated single-quoted value")
		}
		end++

		if err := validateDotEnvTail(raw[end+1:]); err != nil {
			return "", err
		}

		return raw[1:end], nil
	default:
		return trimDotEnvComment(raw), nil
	}
}

func findClosingDoubleQuote(raw string) int {
	escaped := false
	for i := 1; i < len(raw); i++ {
		switch {
		case escaped:
			escaped = false
		case raw[i] == '\\':
			escaped = true
		case raw[i] == '"':
			return i
		}
	}
	return -1
}

func validateDotEnvTail(tail string) error {
	tail = strings.TrimSpace(tail)
	if tail == "" || strings.HasPrefix(tail, "#") {
		return nil
	}
	return fmt.Errorf("unexpected trailing content %q", tail)
}

func trimDotEnvComment(raw string) string {
	for i := 0; i < len(raw); i++ {
		if raw[i] == '#' && (i == 0 || isDotEnvWhitespace(raw[i-1])) {
			return strings.TrimSpace(raw[:i])
		}
	}
	return strings.TrimSpace(raw)
}

func isDotEnvWhitespace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r':
		return true
	default:
		return false
	}
}
