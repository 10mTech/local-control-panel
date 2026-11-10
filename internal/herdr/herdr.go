// Package herdr is the only code that calls herdr. It runs the herdr CLI with
// argument lists, parses its JSON replies, and turns error replies into *Error.
//
// Inside a herdr pane the CLI finds its session through HERDR_SOCKET_PATH,
// which herdr injects, so the panel talks to whichever session it runs in.
package herdr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"local-control-panel/internal/safety"
)

// MinVersion is the oldest herdr whose JSON replies this adapter was written against.
var MinVersion = [3]int{0, 8, 0}

// Error is an error reply from the herdr server, e.g. server_not_running.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	if e.Message == "" {
		return "herdr: " + e.Code
	}
	return "herdr: " + e.Code + ": " + e.Message
}

// IsNotRunning reports whether err means no herdr server is listening.
func IsNotRunning(err error) bool {
	var he *Error
	return errors.As(err, &he) && he.Code == "server_not_running"
}

type Workspace struct {
	ID     string `json:"workspace_id"`
	Label  string `json:"label"`
	Number int    `json:"number"`
}

type Pane struct {
	ID          string `json:"pane_id"`
	WorkspaceID string `json:"workspace_id"`
	TabID       string `json:"tab_id"`
	Cwd         string `json:"cwd"`
	Label       string `json:"label"`
	Focused     bool   `json:"focused"`
}

type Process struct {
	PID  int      `json:"pid"`
	Name string   `json:"name"`
	Argv []string `json:"argv"`
}

type ProcessInfo struct {
	ShellPID        int       `json:"shell_pid"`
	ForegroundGroup int       `json:"foreground_process_group_id"`
	Foreground      []Process `json:"foreground_processes"`
}

// Client runs herdr commands. Trace, when set, receives every command line
// with its error, so callers can put the exact herdr calls in the audit log.
type Client struct {
	Bin   string
	Trace func(argv []string, err error)
}

func New() *Client {
	bin := os.Getenv("HERDR_BIN_PATH")
	if bin == "" {
		bin = "herdr"
	}
	return &Client{Bin: bin}
}

// WithTrace returns a copy of c that appends each call to *calls.
func (c *Client) WithTrace(calls *[]string) *Client {
	cc := *c
	cc.Trace = func(argv []string, err error) {
		s := "herdr " + safety.ShellJoin(argv)
		if err != nil {
			s += "  → " + err.Error()
		}
		*calls = append(*calls, s)
	}
	return &cc
}

// InPane reports whether the panel itself runs inside a herdr pane.
func InPane() bool { return os.Getenv("HERDR_ENV") == "1" }

// Version returns the installed herdr version, e.g. [0 8 2].
func (c *Client) Version(ctx context.Context) ([3]int, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, c.Bin, "--version").Output()
	if err != nil {
		return [3]int{}, "", fmt.Errorf("herdr --version: %w", err)
	}
	s := strings.TrimSpace(string(out))
	m := regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`).FindStringSubmatch(s)
	if m == nil {
		return [3]int{}, s, fmt.Errorf("cannot read a version from %q", s)
	}
	var v [3]int
	for i := range v {
		v[i], _ = strconv.Atoi(m[i+1])
	}
	return v, s, nil
}

// Check confirms herdr is installed, new enough and has a running server.
// It returns a one-line status for the top bar.
func (c *Client) Check(ctx context.Context) (string, error) {
	v, s, err := c.Version(ctx)
	if err != nil {
		return "", err
	}
	if less(v, MinVersion) {
		return s, fmt.Errorf("%s is older than %d.%d.%d", s, MinVersion[0], MinVersion[1], MinVersion[2])
	}
	if _, err := c.Workspaces(ctx); err != nil {
		return s, err
	}
	return s, nil
}

func less(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// call runs one herdr command and decodes .result into out (if not nil).
func (c *Client) call(ctx context.Context, out any, argv ...string) (err error) {
	if c.Trace != nil {
		defer func() { c.Trace(argv, err) }()
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Bin, argv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()

	// Server errors are JSON on stderr (exit 1); some builds print them on stdout.
	for _, b := range [][]byte{stderr.Bytes(), stdout.Bytes()} {
		var reply struct {
			Error *Error `json:"error"`
		}
		if json.Unmarshal(bytes.TrimSpace(b), &reply) == nil && reply.Error != nil {
			return reply.Error
		}
	}
	if runErr != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return fmt.Errorf("herdr %s: %w", argv[0], runErr)
		}
		return fmt.Errorf("herdr %s: %s", argv[0], msg)
	}
	if out == nil || len(bytes.TrimSpace(stdout.Bytes())) == 0 {
		return nil
	}
	var reply struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &reply); err != nil {
		return fmt.Errorf("herdr %s: unreadable reply: %w", argv[0], err)
	}
	if err := json.Unmarshal(reply.Result, out); err != nil {
		return fmt.Errorf("herdr %s: unexpected reply: %w", argv[0], err)
	}
	return nil
}

func (c *Client) Workspaces(ctx context.Context) ([]Workspace, error) {
	var r struct {
		Workspaces []Workspace `json:"workspaces"`
	}
	err := c.call(ctx, &r, "workspace", "list")
	return r.Workspaces, err
}

// CreateWorkspace makes a workspace whose first pane starts in cwd.
func (c *Client) CreateWorkspace(ctx context.Context, cwd, label string, focus bool) (Workspace, Pane, error) {
	var r struct {
		Workspace Workspace `json:"workspace"`
		RootPane  Pane      `json:"root_pane"`
	}
	err := c.call(ctx, &r, "workspace", "create", "--cwd", cwd, "--label", label, focusFlag(focus))
	if err == nil && (r.Workspace.ID == "" || r.RootPane.ID == "") {
		err = errors.New("herdr workspace create: reply has no workspace or pane id")
	}
	return r.Workspace, r.RootPane, err
}

func (c *Client) FocusWorkspace(ctx context.Context, id string) error {
	return c.call(ctx, nil, "workspace", "focus", id)
}

func (c *Client) Panes(ctx context.Context, workspaceID string) ([]Pane, error) {
	var r struct {
		Panes []Pane `json:"panes"`
	}
	err := c.call(ctx, &r, "pane", "list", "--workspace", workspaceID)
	return r.Panes, err
}

// Pane returns one pane, or an error such as pane_not_found once it is closed.
func (c *Client) Pane(ctx context.Context, id string) (Pane, error) {
	var r struct {
		Pane Pane `json:"pane"`
	}
	err := c.call(ctx, &r, "pane", "get", id)
	return r.Pane, err
}

// Split opens a new pane next to pane, starting in cwd.
func (c *Client) Split(ctx context.Context, pane, direction, cwd string, focus bool) (Pane, error) {
	var r struct {
		Pane Pane `json:"pane"`
	}
	err := c.call(ctx, &r, "pane", "split", pane, "--direction", direction, "--cwd", cwd, focusFlag(focus))
	if err == nil && r.Pane.ID == "" {
		err = errors.New("herdr pane split: reply has no pane id")
	}
	return r.Pane, err
}

// Run types command into the pane's shell and presses Enter.
func (c *Client) Run(ctx context.Context, pane, command string) error {
	return c.call(ctx, nil, "pane", "run", pane, command)
}

func (c *Client) Rename(ctx context.Context, pane, label string) error {
	return c.call(ctx, nil, "pane", "rename", pane, label)
}

func (c *Client) SendKeys(ctx context.Context, pane string, keys ...string) error {
	return c.call(ctx, nil, append([]string{"pane", "send-keys", pane}, keys...)...)
}

func (c *Client) ProcessInfo(ctx context.Context, pane string) (ProcessInfo, error) {
	var r struct {
		Info ProcessInfo `json:"process_info"`
	}
	err := c.call(ctx, &r, "pane", "process-info", "--pane", pane)
	return r.Info, err
}

func focusFlag(focus bool) string {
	if focus {
		return "--focus"
	}
	return "--no-focus"
}
