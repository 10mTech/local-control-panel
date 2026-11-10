package ui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func humanBytes(n int64) string {
	const k = 1024
	switch {
	case n < k*k:
		return fmt.Sprintf("%dK", (n+k-1)/k)
	case n < 10*k*k*k:
		v := float64(n) / (k * k)
		if v >= 1000 {
			return fmt.Sprintf("%.1fG", v/k)
		}
		return fmt.Sprintf("%.0fM", v)
	default:
		return fmt.Sprintf("%.0fG", float64(n)/(k*k*k))
	}
}

// age is a short "how long ago": 5m, 3h, 4d, 7mo, 2y.
func age(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 2*365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/24/30))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/24/365))
	}
}

// ago is age with "ago", or "just now" under a minute.
func ago(t time.Time) string {
	if time.Since(t) < time.Minute {
		return "just now"
	}
	return age(t) + " ago"
}

// fit truncates s to w cells with an ellipsis and pads it to exactly w.
// home is shown as ~ in every path on screen, so the user name never appears
// (handy for screenshots and recordings).
var home, _ = os.UserHomeDir()

func tilde(s string) string {
	if home == "" || home == "/" {
		return s
	}
	return strings.ReplaceAll(s, home+"/", "~/")
}

func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = tilde(s)
	if ansi.StringWidth(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
}

// fitLeft is fit, right-aligned.
func fitLeft(s string, w int) string {
	s = tilde(s)
	if ansi.StringWidth(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	return strings.Repeat(" ", max(0, w-ansi.StringWidth(s))) + s
}

// fuzzy reports whether every rune of q appears in s, in order.
func fuzzy(q, s string) bool {
	q, s = strings.ToLower(q), strings.ToLower(s)
	i := 0
	for _, r := range s {
		if i < len(q) && rune(q[i]) == r {
			i++
		}
	}
	return i == len(q)
}
