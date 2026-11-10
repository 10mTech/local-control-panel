package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newLogger(t *testing.T) *Logger {
	t.Helper()
	l, err := New(t.TempDir(), "test")
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func writeEvents(t *testing.T, l *Logger, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := l.Event(Entry{Action: "e" + string(rune('a'+i))}); err != nil {
			t.Fatal(err)
		}
	}
}

// monthFile returns the only month file in the log directory.
func monthFile(t *testing.T, l *Logger) string {
	t.Helper()
	files, err := monthFiles(l.Dir())
	if err != nil || len(files) != 1 {
		t.Fatalf("month files = %v, %v; want exactly one", files, err)
	}
	return files[0]
}

func TestReadAllEmpty(t *testing.T) {
	recs, chain, err := ReadAll(newLogger(t).Dir())
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 || !chain.OK || chain.Lines != 0 {
		t.Errorf("empty log: recs=%d chain=%+v", len(recs), chain)
	}
}

func TestReadAllChainOK(t *testing.T) {
	l := newLogger(t)
	op, err := l.Begin(Entry{Action: "clean", Project: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if err := op.End("ok", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	writeEvents(t, l, 3)

	recs, chain, err := ReadAll(l.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if !chain.OK || chain.Lines != 5 || len(recs) != 5 {
		t.Fatalf("chain=%+v recs=%d, want OK with 5 lines", chain, len(recs))
	}
	if recs[0].Phase != "intent" || recs[1].Phase != "result" || recs[0].ID != recs[1].ID {
		t.Errorf("intent/result pair wrong: %+v %+v", recs[0].Entry, recs[1].Entry)
	}
	if recs[0].Prev != "" {
		t.Errorf("first line prev = %q, want empty", recs[0].Prev)
	}
	if recs[4].Line != 5 {
		t.Errorf("last record line = %d, want 5", recs[4].Line)
	}
}

func TestReadAllEditedLineBreaksChain(t *testing.T) {
	l := newLogger(t)
	writeEvents(t, l, 4)
	path := monthFile(t, l)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	lines[1] = strings.Replace(lines[1], `"action":"eb"`, `"action":"evil"`, 1)
	if lines[1] == strings.Split(strings.TrimRight(string(b), "\n"), "\n")[1] {
		t.Fatal("test did not edit the line")
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, chain, err := ReadAll(l.Dir())
	if err != nil {
		t.Fatal(err)
	}
	// Line 3 carries the hash of the original line 2, so the break shows there.
	if chain.OK || chain.BrokenAt != filepath.Base(path)+":3" {
		t.Errorf("chain = %+v, want broken at %s:3", chain, filepath.Base(path))
	}
}

func TestReadAllDeletedLineBreaksChain(t *testing.T) {
	l := newLogger(t)
	writeEvents(t, l, 4)
	path := monthFile(t, l)
	b, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	lines = append(lines[:1], lines[2:]...)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, chain, _ := ReadAll(l.Dir())
	if chain.OK || chain.BrokenAt != filepath.Base(path)+":2" {
		t.Errorf("chain = %+v, want broken at %s:2", chain, filepath.Base(path))
	}
}

func TestReadAllGarbageLineBreaksChain(t *testing.T) {
	l := newLogger(t)
	writeEvents(t, l, 2)
	path := monthFile(t, l)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("not json\n")
	f.Close()

	_, chain, _ := ReadAll(l.Dir())
	if chain.OK || chain.BrokenAt != filepath.Base(path)+":3" {
		t.Errorf("chain = %+v, want broken at %s:3", chain, filepath.Base(path))
	}
}

func TestChainLinksAcrossMonthFiles(t *testing.T) {
	l := newLogger(t)
	writeEvents(t, l, 2)
	path := monthFile(t, l)
	old := filepath.Join(l.Dir(), "2000-01.jsonl")
	if err := os.Rename(path, old); err != nil {
		t.Fatal(err)
	}
	writeEvents(t, l, 2) // starts a new month file that must link to 2000-01

	recs, chain, err := ReadAll(l.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if !chain.OK || chain.Lines != 4 {
		t.Fatalf("chain = %+v, want OK with 4 lines", chain)
	}
	if recs[0].File != "2000-01.jsonl" || recs[2].File == "2000-01.jsonl" {
		t.Errorf("files = %s, %s; want the old file first, then a new one", recs[0].File, recs[2].File)
	}
	if recs[2].Prev == "" {
		t.Error("first line of the new month file does not link to the old file")
	}

	// Editing the old file breaks the link at the start of the new one.
	b, _ := os.ReadFile(old)
	b = []byte(strings.Replace(string(b), `"action":"eb"`, `"action":"evil"`, 1))
	if err := os.WriteFile(old, b, 0o600); err != nil {
		t.Fatal(err)
	}
	_, chain, _ = ReadAll(l.Dir())
	if chain.OK || !strings.HasSuffix(chain.BrokenAt, ":1") || strings.HasPrefix(chain.BrokenAt, "2000-01") {
		t.Errorf("chain = %+v, want broken at line 1 of the new month file", chain)
	}
}
