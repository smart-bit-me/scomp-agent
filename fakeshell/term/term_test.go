package term

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestDecoder(t *testing.T) {
	cases := []struct {
		in   []string // chunks
		want []Key
	}{
		{[]string{"ab"}, []Key{'a', 'b'}},
		{[]string{"\x1b[A\x1bOB\x1b[C\x1bOD"}, []Key{KeyUp, KeyDown, KeyRight, KeyLeft}},
		{[]string{"\x1b[", "A"}, []Key{KeyUp}},
		{[]string{"\x1b[3~\x1b[1~\x1b[4~\x1b[H\x1bOF"}, []Key{KeyDelete, KeyHome, KeyEnd, KeyHome, KeyEnd}},
		{[]string{"x\r\ny\rz\n"}, []Key{'x', KeyEnter, 'y', KeyEnter, 'z', KeyEnter}},
		{[]string{"\x7f\x08\x03"}, []Key{KeyBackspace, KeyBackspace, KeyCtrlC}},
		{[]string{"\x1b"}, []Key{KeyEsc}},
		{[]string{"\x1bx"}, []Key{KeyEsc, 'x'}},
		{[]string{"\xc3", "\xa9"}, []Key{'é'}},
		{[]string{"\x1b[" + strings.Repeat("9", 40) + "Aq"}, []Key{KeyUnknown, 'q'}},
	}
	for _, c := range cases {
		var d Decoder
		var got []Key
		for _, chunk := range c.in {
			got = append(got, d.Feed([]byte(chunk))...)
		}
		if len(got) != len(c.want) {
			t.Errorf("%q: got %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q: key %d = %v, want %v", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func editorWith(input string, width int) (*Editor, *strings.Builder) {
	var d Decoder
	keys := make(chan Key, 256)
	for _, k := range d.Feed([]byte(input)) {
		keys <- k
	}
	out := &strings.Builder{}
	return &Editor{Keys: keys, Out: out, Width: func() int { return width }}, out
}

func TestEditorEditing(t *testing.T) {
	cases := map[string]string{
		"hello\r":                  "hello",
		"helo\x1b[Dl\r":            "hello",
		"abc\x7f\x7fx\r":           "ax",
		"one two\x17three\r":       "one three",
		"junk\x15clean\r":          "clean",
		"abcdef\x01\x1b[C\x0b\r":   "a",
		"bc\x01a\x05d\r":           "abcd",
		"x\x1b[3~\x1b[D\x1b[3~y\r": "y",
	}
	for in, want := range cases {
		e, _ := editorWith(in, 80)
		got, err := e.ReadLine(context.Background(), "$ ")
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestEditorHistory(t *testing.T) {
	e, _ := editorWith("first\rsecond\r\x1b[A\x1b[A\r\x1b[A\x1b[B\r", 80)
	var got []string
	for i := 0; i < 4; i++ {
		line, err := e.ReadLine(context.Background(), "$ ")
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, line)
	}
	want := []string{"first", "second", "first", ""}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
	if h := e.History(); len(h) != 3 || h[2] != "first" {
		t.Errorf("history = %q", h)
	}
}

func TestEditorControlResults(t *testing.T) {
	e, out := editorWith("abc\x03", 80)
	if _, err := e.ReadLine(context.Background(), "$ "); !errors.Is(err, ErrInterrupt) {
		t.Errorf("Ctrl-C err = %v", err)
	}
	if !strings.Contains(out.String(), "^C") {
		t.Errorf("Ctrl-C not echoed: %q", out.String())
	}
	e, _ = editorWith("\x04", 80)
	if _, err := e.ReadLine(context.Background(), "$ "); !errors.Is(err, io.EOF) {
		t.Errorf("Ctrl-D err = %v", err)
	}
	e, _ = editorWith("", 80)
	e.Idle = 20 * time.Millisecond
	if _, err := e.ReadLine(context.Background(), "$ "); !errors.Is(err, ErrIdle) {
		t.Errorf("idle err = %v", err)
	}
	e, _ = editorWith(strings.Repeat("x", 50)+"\r", 80)
	e.MaxLen = 10
	if got, _ := e.ReadLine(context.Background(), "$ "); got != strings.Repeat("x", 10) {
		t.Errorf("MaxLen not enforced: %d chars", len(got))
	}
}

func TestEditorCompletion(t *testing.T) {
	e, _ := editorWith("ca\t\r", 80)
	e.Complete = func(line []rune, pos int) ([]rune, int, []string) {
		return []rune("cat "), 4, nil
	}
	if got, _ := e.ReadLine(context.Background(), "$ "); got != "cat " {
		t.Errorf("completed line = %q", got)
	}
	e, out := editorWith("c\t\r", 80)
	e.Complete = func(line []rune, pos int) ([]rune, int, []string) {
		return line, pos, []string{"cat", "cd"}
	}
	e.ReadLine(context.Background(), "$ ")
	if !strings.Contains(out.String(), "cat  cd") {
		t.Errorf("choices not listed: %q", out.String())
	}
}

// TestEditorWrapping replays the editor's output into a tiny screen model and
// checks the visible text after edits on a line wider than the terminal.
func TestEditorWrapping(t *testing.T) {
	const width = 10
	input := "abcdefghijklmnopqrstuvwxy" + "\x1b[D\x1b[D\x1b[D" + "XY" + "\x01" + "#"
	e, out := editorWith(input+"\r", width)
	got, err := e.ReadLine(context.Background(), "$ ")
	if err != nil {
		t.Fatal(err)
	}
	want := "#abcdefghijklmnopqrstuvXYwxy"
	if got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
	screen := render(out.String(), width)
	if text := strings.Join(screen, ""); !strings.HasPrefix(text, "$ "+want) {
		t.Errorf("screen = %q", screen)
	}
}

// render is a minimal VT model: printable chars, CR, LF, CUU, CUF, ED.
func render(s string, width int) []string {
	rows := [][]rune{make([]rune, width)}
	r, c, pending := 0, 0, false
	ensure := func() {
		for len(rows) <= r {
			rows = append(rows, make([]rune, width))
		}
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == '\r':
			c, pending = 0, false
		case ch == '\n':
			r++
			pending = false
			ensure()
		case ch == 0x1b && i+1 < len(s) && s[i+1] == '[':
			j := i + 2
			n := 0
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				n = n*10 + int(s[j]-'0')
				j++
			}
			if n == 0 {
				n = 1
			}
			switch s[j] {
			case 'A':
				r = max(0, r-n)
			case 'C':
				c = min(width-1, c+n)
			case 'J':
				for x := c; x < width; x++ {
					rows[r][x] = 0
				}
				rows = rows[:r+1]
			}
			pending = false
			i = j
		default:
			if pending {
				r, c, pending = r+1, 0, false
				ensure()
			}
			rows[r][c] = rune(ch)
			if c == width-1 {
				pending = true
			} else {
				c++
			}
		}
	}
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = strings.TrimRight(strings.Map(func(r rune) rune {
			if r == 0 {
				return ' '
			}
			return r
		}, string(row)), " ")
	}
	return out
}
