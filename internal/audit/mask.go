package audit

import (
	"bytes"
	"encoding/json"
	"regexp"
)

var maskers = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`(ghp_|gho_|ghs_|ghu_|ghr_|github_pat_|glpat-|sk-|sk_live_|sk_test_|rk_live_|xox[abprs]-)[A-Za-z0-9_\-]{6,}`), "${1}****"},
	{regexp.MustCompile(`\b(AKIA|ASIA)[A-Z0-9]{16}\b`), "${1}****"},
	{regexp.MustCompile(`(?i)(bearer\s+)\S+`), "${1}****"},
	// NAME=value where the name says it is a secret, e.g. STRIPE_KEY=, DB_PASSWORD=.
	{regexp.MustCompile(`(?i)(\b[A-Z0-9_.\-]*(?:password|passwd|pwd|token|secret|key|credentials?|auth)=)[^\s&'"]+`), "${1}****"},
	// --password value, --token=value and the like.
	{regexp.MustCompile(`(?i)(--?[a-z0-9\-]*(?:password|passwd|token|secret|api-?key|access-?key)(?:=|\s+))[^\s'"-][^\s'"]*`), "${1}****"},
	{regexp.MustCompile(`(://)[^/\s:@]+:[^/\s@]+@`), "${1}****@"},
	{regexp.MustCompile(`(://)[^/\s:@]{20,}@`), "${1}****@"}, // https://TOKEN@host
}

// Mask hides tokens, bearer headers, key=value secrets, secret flags and URL
// credentials.
func Mask(s string) string {
	for _, m := range maskers {
		s = m.re.ReplaceAllString(s, m.repl)
	}
	return s
}

// MaskArgv masks every word of a command, and the word after a secret flag
// such as --password.
func MaskArgv(argv []string) []string {
	if argv == nil {
		return nil
	}
	out := make([]string, len(argv))
	for i, a := range argv {
		out[i] = Mask(a)
	}
	// A secret flag with its value in the next word: --password hunter2.
	for i := 0; i+1 < len(out); i++ {
		if secretFlag.MatchString(out[i]) {
			out[i+1] = "****"
		}
	}
	return out
}

var secretFlag = regexp.MustCompile(`(?i)^--?[a-z0-9\-]*(?:password|passwd|token|secret|api-?key|access-?key)$`)

// maskDetails masks every string in d, however deeply it is nested in maps,
// slices or structs, by going through JSON. Details are written as JSON
// anyway, so nothing is lost.
func maskDetails(d map[string]any) map[string]any {
	if d == nil {
		return nil
	}
	b, err := json.Marshal(d)
	if err != nil {
		return map[string]any{"unmaskable": err.Error()}
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber() // keep integers such as sizes exact
	if err := dec.Decode(&v); err != nil {
		return map[string]any{"unmaskable": err.Error()}
	}
	out, _ := maskValue(v).(map[string]any)
	return out
}

func maskValue(v any) any {
	switch t := v.(type) {
	case string:
		return Mask(t)
	case []any:
		for i := range t {
			t[i] = maskValue(t[i])
		}
		return t
	case map[string]any:
		for k, x := range t {
			t[k] = maskValue(x)
		}
		return t
	}
	return v
}
