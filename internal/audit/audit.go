// Package audit writes the append-only, hash-chained audit log.
//
// Every action writes an intent line before it runs and a result line after,
// sharing one id. Lines go to <state>/audit/YYYY-MM.jsonl. Each line carries
// "prev", the sha256 of the previous line's bytes (across month files), so an
// edited or deleted line breaks the chain and ReadAll reports where.
package audit

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/oklog/ulid/v2"
)

type Entry struct {
	TS         string         `json:"ts"`
	ID         string         `json:"id"`
	Phase      string         `json:"phase"` // intent | result | event
	Action     string         `json:"action"`
	Project    string         `json:"project,omitempty"`
	Remote     string         `json:"remote,omitempty"`
	Cwd        string         `json:"cwd,omitempty"`
	Argv       []string       `json:"argv,omitempty"`
	Trigger    string         `json:"trigger,omitempty"`
	Outcome    string         `json:"outcome,omitempty"` // ok | failed | cancelled
	ExitCode   *int           `json:"exit_code,omitempty"`
	DurationMS *int64         `json:"duration_ms,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
	Error      string         `json:"error,omitempty"`
	AppVersion string         `json:"app_version"`
	Prev       string         `json:"prev"`
}

type Logger struct {
	dir     string
	version string
	mu      sync.Mutex
}

func New(stateDir, version string) (*Logger, error) {
	dir := filepath.Join(stateDir, "audit")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Logger{dir: dir, version: version}, nil
}

func (l *Logger) Dir() string { return l.dir }

// Op is one audited action between its intent and result lines.
type Op struct {
	l       *Logger
	base    Entry
	started time.Time
}

// Begin writes the intent line. The returned Op must be ended with End.
func (l *Logger) Begin(e Entry) (*Op, error) {
	e.ID = ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
	e.Phase = "intent"
	op := &Op{l: l, base: e, started: time.Now()}
	return op, l.write(e)
}

// End writes the result line for the op.
func (op *Op) End(outcome string, exitCode *int, details map[string]any, err error) error {
	e := op.base
	e.Phase = "result"
	e.Outcome = outcome
	e.ExitCode = exitCode
	d := time.Since(op.started).Milliseconds()
	e.DurationMS = &d
	e.Details = details
	if err != nil {
		e.Error = err.Error()
	}
	return op.l.write(e)
}

// Event writes a single line for things with no separate result, such as a
// panel session starting.
func (l *Logger) Event(e Entry) error {
	e.ID = ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
	e.Phase = "event"
	return l.write(e)
}

func (l *Logger) write(e Entry) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	e.TS = now.Format("2006-01-02T15:04:05.000Z07:00")
	e.AppVersion = l.version
	e.Remote = Mask(e.Remote)
	e.Argv = MaskArgv(e.Argv)
	e.Error = Mask(e.Error)
	e.Details = maskDetails(e.Details)

	name := filepath.Join(l.dir, now.Format("2006-01")+".jsonl")
	f, err := os.OpenFile(name, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	// Lock across processes so two panels never interleave or fork the chain.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	prev, err := lastLine(f)
	if err != nil {
		return err
	}
	if prev == nil {
		prev, err = l.lastLineBefore(filepath.Base(name))
		if err != nil {
			return err
		}
	}
	e.Prev = hashLine(prev)

	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

// lastLineBefore returns the last line of the newest month file older than name.
func (l *Logger) lastLineBefore(name string) ([]byte, error) {
	files, err := monthFiles(l.dir)
	if err != nil {
		return nil, err
	}
	for i := len(files) - 1; i >= 0; i-- {
		if filepath.Base(files[i]) >= name {
			continue
		}
		f, err := os.Open(files[i])
		if err != nil {
			return nil, err
		}
		line, err := lastLine(f)
		f.Close()
		if err != nil {
			return nil, err
		}
		if line != nil {
			return line, nil
		}
	}
	return nil, nil
}

func monthFiles(dir string) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// hashLine is the chain link for a line; the first line ever links to "".
func hashLine(line []byte) string {
	if line == nil {
		return ""
	}
	sum := sha256.Sum256(line)
	return hex.EncodeToString(sum[:])
}

// lastLine returns the final non-empty line of f without its newline, or nil.
func lastLine(f *os.File) ([]byte, error) {
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := st.Size()
	if size == 0 {
		return nil, nil
	}
	const chunk = 4096
	var buf []byte
	for pos := size; pos > 0; {
		n := min(int64(chunk), pos)
		pos -= n
		b := make([]byte, n)
		if _, err := f.ReadAt(b, pos); err != nil && err != io.EOF {
			return nil, err
		}
		buf = append(b, buf...)
		t := bytes.TrimRight(buf, "\n")
		if i := bytes.LastIndexByte(t, '\n'); i >= 0 {
			return t[i+1:], nil
		}
		if pos == 0 && len(t) > 0 {
			return t, nil
		}
	}
	return nil, nil
}
