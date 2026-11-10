package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMask(t *testing.T) {
	cases := []struct{ in, want string }{
		{"ghp_abcdefghijklmnop", "ghp_****"},
		{"github_pat_11AAAAAA_xyz", "github_pat_****"},
		{"sk_live_abcdefgh1234", "sk_live_****"},
		{"AKIAABCDEFGHIJKLMNOP", "AKIA****"},
		{"Authorization: Bearer abc.def.ghi", "Authorization: Bearer ****"},
		{"API_KEY=hunter2 npm run dev", "API_KEY=**** npm run dev"},
		{"STRIPE_KEY=abc DB_PASSWORD=pw", "STRIPE_KEY=**** DB_PASSWORD=****"},
		{"AWS_SECRET_ACCESS_KEY=abc", "AWS_SECRET_ACCESS_KEY=****"},
		{"tool --password hunter2 --verbose", "tool --password **** --verbose"},
		{"tool --token=abc123", "tool --token=****"},
		{"https://user:s3cret@github.com/acme/x.git", "https://****@github.com/acme/x.git"},
		{"https://ghp_abcdefghijklmnopqrstuv@github.com/acme/x.git", "https://ghp_****@github.com/acme/x.git"},
		{"https://abcdefghijklmnopqrstuvwxyz@host/x", "https://****@host/x"},
		{"git@github.com:acme/x.git", "git@github.com:acme/x.git"},
		{"npm run dev", "npm run dev"},
		{"PORT=3000 npm run dev", "PORT=3000 npm run dev"},
	}
	for _, c := range cases {
		if got := Mask(c.in); got != c.want {
			t.Errorf("Mask(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMaskArgv(t *testing.T) {
	got := MaskArgv([]string{"env", "API_KEY=abc", "tool", "--password", "hunter2", "-v"})
	want := []string{"env", "API_KEY=****", "tool", "--password", "****", "-v"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("MaskArgv = %q, want %q", got, want)
	}
	if MaskArgv(nil) != nil {
		t.Error("MaskArgv(nil) should stay nil")
	}
}

func TestMaskDetailsNested(t *testing.T) {
	type item struct {
		Out string `json:"out"`
	}
	d := map[string]any{
		"herdr":   []string{"herdr pane run p1 'env API_KEY=abc npm run dev'"},
		"results": []item{{Out: "token=xyz"}},
		"nested":  map[string]any{"remote": "https://u:pw@host/x"},
		"size":    int64(1) << 60,
	}
	b, _ := json.Marshal(maskDetails(d))
	s := string(b)
	for _, secret := range []string{"abc", "xyz", "u:pw"} {
		if strings.Contains(s, secret) {
			t.Errorf("secret %q survived masking: %s", secret, s)
		}
	}
	if !strings.Contains(s, "1152921504606846976") {
		t.Errorf("large integer changed: %s", s)
	}
}

func TestWriteMasksRemote(t *testing.T) {
	l, err := New(t.TempDir(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Event(Entry{Action: "x", Remote: "https://u:pw@github.com/acme/x.git"}); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(l.Dir(), "*.jsonl"))
	b, _ := os.ReadFile(files[0])
	if strings.Contains(string(b), "u:pw") {
		t.Errorf("remote credentials written: %s", b)
	}
}
