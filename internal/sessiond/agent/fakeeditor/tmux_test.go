package fakeeditor

import (
	"context"
	"strings"
	"testing"

	"github.com/floatshadow/emacs-playground/internal/sessiond/supervisor"
)

// FakeTmux reads back what supervisor.TmuxKeys writes.
func TestFakeTmuxKeys(t *testing.T) {
	for _, seq := range []string{"C-x C-f", "RET", "TAB", "SPC", "ESC", "DEL", "C-SPC", "M-x", "C-M-f", "<up>", "<f3>", "<insert>", "M-;", "C-x 2"} {
		tk, err := supervisor.TmuxKeys(seq)
		if err != nil {
			t.Fatalf("%s: %v", seq, err)
		}
		var back []string
		for _, k := range tk {
			back = append(back, emacsKey(k))
		}
		if got := strings.Join(back, " "); got != seq {
			t.Errorf("%s → %v → %s", seq, tk, got)
		}
	}
	e := New(t.TempDir())
	ft := FakeTmux{Editor: e}
	tk, _ := supervisor.TmuxKeys("C-x 2")
	if err := ft.SendKeys(context.Background(), false, tk...); err != nil {
		t.Fatal(err)
	}
	if err := ft.SendKeys(context.Background(), true, "hi"); err != nil {
		t.Fatal(err)
	}
	m := e.Snapshot()
	if len(m.Windows) != 2 || m.Buffers[0].Text != "hi" {
		t.Fatalf("windows %v, *scratch* %q", m.Windows, m.Buffers[0].Text)
	}
}
