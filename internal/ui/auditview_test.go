package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/10mTech/local-control-panel/internal/audit"
)

// The audit screen must fit the terminal exactly; a taller view pushes its
// header line (with the chain status) off the top.
func TestAuditScreenFitsHeight(t *testing.T) {
	lg, err := audit.New(t.TempDir(), "test")
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range []int{24, 34, 50} {
		m := &Model{width: 110, height: h, audit: lg}
		out := m.auditScreen()
		if got := lipgloss.Height(out); got != h {
			t.Errorf("height %d: audit screen is %d lines", h, got)
		}
		if first := strings.SplitN(out, "\n", 2)[0]; !strings.Contains(first, "audit") {
			t.Errorf("height %d: first line %q, want the audit header", h, first)
		}
	}
}
