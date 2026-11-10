// Package safety holds the checks every action passes before it touches
// anything. They are fixed in code, not settings.
package safety

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Inside returns an error unless path resolves inside one of the roots.
// Symlinks are resolved, so a link cannot lead an action out of the folder.
func Inside(path string, roots ...string) error {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	for _, r := range roots {
		rr, err := filepath.EvalSymlinks(r)
		if err != nil {
			continue
		}
		if real == rr || strings.HasPrefix(real, rr+string(filepath.Separator)) {
			return nil
		}
	}
	return fmt.Errorf("refused: %s is outside %s", path, strings.Join(roots, ", "))
}

// ShellQuote quotes one word for a POSIX shell (sh, bash, zsh) with single
// quotes, so spaces and $ in paths like "My Projects" stay literal. Not for
// fish: it reads \\ inside single quotes as one backslash.
func ShellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./:=@+,", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ShellJoin quotes every word and joins them with spaces.
func ShellJoin(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = ShellQuote(a)
	}
	return strings.Join(q, " ")
}

// SplitWords splits a command typed by the user into words, honouring single
// and double quotes and backslash escapes. It never expands anything.
func SplitWords(s string) ([]string, error) {
	var words []string
	var cur strings.Builder
	inWord := false
	var quote rune
	escaped := false
	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case quote != 0:
			if r == quote {
				quote = 0
			} else if r == '\\' && quote == '"' {
				escaped = true
			} else {
				cur.WriteRune(r)
			}
		case r == '\\':
			escaped, inWord = true, true
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == ' ' || r == '\t':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unclosed %c quote", quote)
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words, nil
}
