package session

import (
	"strings"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/steps"
)

// stuckLocked evaluates the step's stuck rules on the attempt's statistics
// (edge-triggered: a rule fires when it becomes true and re-arms when it
// becomes false).
func (c *Core) stuckLocked(step *protocol.Step, st steps.Stats, now time.Time) []protocol.Event {
	var evs []protocol.Event
	for i := range step.Stuck {
		r := &step.Stuck[i]
		id := r.ID
		if id == "" {
			id = protocol.StuckRuleID(step.ID, i)
		}
		holds := ruleHolds(&r.When, st, now)
		if holds && !c.obs.nudged[id] {
			c.obs.nudged[id] = true
			body := r.Say.Get(c.d.Locale)
			title := r.Title.Get(c.d.Locale)
			if title == "" {
				title = firstSentence(body)
			}
			kind := r.Kind
			if kind == "" {
				kind = protocol.NudgeCalm
			}
			evs = append(evs, &protocol.NudgeEvent{ID: id, Kind: kind, Title: title, Body: body})
		} else if !holds {
			c.obs.nudged[id] = false
		}
	}
	return evs
}

// ruleHolds reports whether every condition the rule sets holds (a rule
// that sets none never does).
func ruleHolds(w *protocol.StuckWhen, st steps.Stats, now time.Time) bool {
	set := false
	if w.MinibufferSeconds > 0 {
		set = true
		if st.MiniSince.IsZero() || now.Sub(st.MiniSince) < secs(w.MinibufferSeconds) {
			return false
		}
	}
	if w.IdleSeconds > 0 {
		set = true
		last := st.StartedAt
		if st.LastKeyAt.After(last) {
			last = st.LastKeyAt
		}
		if now.Sub(last) < secs(w.IdleSeconds) {
			return false
		}
	}
	if w.StepSeconds > 0 {
		set = true
		if now.Sub(st.StartedAt) < secs(w.StepSeconds) {
			return false
		}
	}
	if q := w.QuitsInWindow; q != nil && q.Count > 0 {
		set = true
		n := 0
		for _, t := range st.Quits {
			if now.Sub(t) <= secs(q.Seconds) {
				n++
			}
		}
		if n < q.Count {
			return false
		}
	}
	if w.UnboundRepeats > 0 {
		set = true
		if st.UnboundRun < w.UnboundRepeats {
			return false
		}
	}
	return set
}

func secs(n int) time.Duration { return time.Duration(n) * time.Second }

func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	for i, r := range s {
		if (r == '.' || r == '?' || r == '!') && (i+1 == len(s) || s[i+1] == ' ' || s[i+1] == '\n') {
			return s[:i+1]
		}
	}
	return s
}
