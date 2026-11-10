package safety

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func mkdir(t *testing.T, parts ...string) string {
	t.Helper()
	p := filepath.Join(parts...)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestInside(t *testing.T) {
	base := t.TempDir()
	root := mkdir(t, base, "root")
	sub := mkdir(t, root, "app", "src")
	lookalike := mkdir(t, base, "root2")
	outside := mkdir(t, base, "outside")

	linkOut := filepath.Join(root, "escape")
	if err := os.Symlink(outside, linkOut); err != nil {
		t.Fatal(err)
	}
	linkRoot := filepath.Join(base, "viaLink")
	if err := os.Symlink(root, linkRoot); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		path  string
		roots []string
		ok    bool
	}{
		{"root itself", root, []string{root}, true},
		{"nested", sub, []string{root}, true},
		{"look-alike prefix", lookalike, []string{root}, false},
		{"symlink out of the root", linkOut, []string{root}, false},
		{"root reached through a symlink", sub, []string{linkRoot}, true},
		{"path reached through a symlinked root", filepath.Join(linkRoot, "app"), []string{root}, true},
		{"dot-dot out of the root", filepath.Join(sub, "..", "..", "..", "outside"), []string{root}, false},
		{"dot-dot staying inside", filepath.Join(sub, "..", ".."), []string{root}, true},
		{"missing path", filepath.Join(root, "nope"), []string{root}, false},
		{"missing root", sub, []string{filepath.Join(base, "gone")}, false},
		{"no roots", sub, nil, false},
		{"second root matches", outside, []string{root, outside}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Inside(tc.path, tc.roots...)
			if (err == nil) != tc.ok {
				t.Fatalf("Inside(%q, %q) = %v, want ok=%v", tc.path, tc.roots, err, tc.ok)
			}
		})
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", "''"},
		{"plain", "plain"},
		{"/a/b-c_d.e:f=g@h+i,j", "/a/b-c_d.e:f=g@h+i,j"},
		{"My Projects", "'My Projects'"},
		{"$HOME", "'$HOME'"},
		{"a;b", "'a;b'"},
		{"it's", `'it'\''s'`},
		{"`x`", "'`x`'"},
		{"a\nb", "'a\nb'"},
	}
	for _, tc := range tests {
		if got := ShellQuote(tc.in); got != tc.want {
			t.Errorf("ShellQuote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSplitWords(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"a b  c", []string{"a", "b", "c"}},
		{"a\tb", []string{"a", "b"}},
		{`'a b' c`, []string{"a b", "c"}},
		{`"a b" c`, []string{"a b", "c"}},
		{`a\ b`, []string{"a b"}},
		{`"a\"b"`, []string{`a"b`}},
		{`'a\b'`, []string{`a\b`}},
		{`""`, []string{""}},
		{`a '' b`, []string{"a", "", "b"}},
		{`a"b c"d`, []string{"ab cd"}},
		{`$HOME *`, []string{"$HOME", "*"}},
	}
	for _, tc := range tests {
		got, err := SplitWords(tc.in)
		if err != nil {
			t.Errorf("SplitWords(%q) error: %v", tc.in, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("SplitWords(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSplitWordsUnclosedQuote(t *testing.T) {
	for _, in := range []string{`'a`, `"a b`, `a "b`} {
		if _, err := SplitWords(in); err == nil {
			t.Errorf("SplitWords(%q): expected an error", in)
		}
	}
}

func TestShellJoinRoundTrip(t *testing.T) {
	cases := [][]string{
		{"go", "test", "./..."},
		{"echo", "My Projects", "it's", `say "hi"`, "$HOME", "a;b", `back\slash`},
		{"x", "", "y"},
		{"tab\there", "new\nline"},
		{"'", `"`, `\`},
	}
	for _, argv := range cases {
		line := ShellJoin(argv)
		got, err := SplitWords(line)
		if err != nil {
			t.Errorf("SplitWords(ShellJoin(%q)) = %q: %v", argv, line, err)
			continue
		}
		if !reflect.DeepEqual(got, argv) {
			t.Errorf("round trip of %q via %q gave %q", argv, line, got)
		}
	}
}
