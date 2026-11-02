package term

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
)

// Errors returned by ReadLine besides io.EOF (Ctrl-D on an empty line, or the
// key channel closed) and context errors.
var (
	ErrInterrupt = errors.New("interrupted") // Ctrl-C
	ErrIdle      = errors.New("idle")        // no key for Editor.Idle
)

// DefaultMaxLen bounds one input line.
const DefaultMaxLen = 1024

const maxHistory = 200

// Completer proposes a completion for line at pos. It returns the new line
// and cursor, plus the choices to list when the completion is ambiguous.
type Completer func(line []rune, pos int) (newLine []rune, newPos int, choices []string)

// Editor reads lines with bash-like editing: cursor movement, history,
// Tab completion, Ctrl-A/E/K/U/W/L/C/D. The redraw is aware of line wrapping,
// so long lines stay correct on narrow phone screens.
type Editor struct {
	Keys     <-chan Key
	Out      io.Writer
	Width    func() int
	Clear    func()        // clears the screen (Ctrl-L); nil writes ED 2
	Idle     time.Duration // 0 disables the idle timeout
	MaxLen   int
	Complete Completer

	history   []string
	cursorRow int // rows between the prompt's first row and the cursor
}

// History returns the recorded lines, oldest first.
func (e *Editor) History() []string { return append([]string(nil), e.history...) }

// ResetHistory forgets every recorded line.
func (e *Editor) ResetHistory() { e.history = nil }

func (e *Editor) addHistory(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	if n := len(e.history); n > 0 && e.history[n-1] == line {
		return
	}
	e.history = append(e.history, line)
	if len(e.history) > maxHistory {
		e.history = e.history[len(e.history)-maxHistory:]
	}
}

func (e *Editor) width() int {
	if e.Width != nil {
		if w := e.Width(); w > 0 {
			return w
		}
	}
	return 80
}

// ReadLine shows prompt and returns the entered line without its newline.
// The cursor is assumed to be at column 0 when it is called.
func (e *Editor) ReadLine(ctx context.Context, prompt string) (string, error) {
	maxLen := e.MaxLen
	if maxLen <= 0 {
		maxLen = DefaultMaxLen
	}
	var (
		line    []rune
		pos     int
		histIdx = len(e.history)
		draft   []rune
	)
	e.cursorRow = 0
	e.render(prompt, line, pos)

	var idle <-chan time.Time
	var timer *time.Timer
	if e.Idle > 0 {
		timer = time.NewTimer(e.Idle)
		defer timer.Stop()
		idle = timer.C
	}

	for {
		var k Key
		var ok bool
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-idle:
			return "", ErrIdle
		case k, ok = <-e.Keys:
			if !ok {
				return "", io.EOF
			}
		}
		if timer != nil {
			timer.Reset(e.Idle)
		}

		switch {
		case k == KeyEnter:
			e.render(prompt, line, len(line))
			io.WriteString(e.Out, "\r\n")
			e.cursorRow = 0
			s := string(line)
			e.addHistory(s)
			return s, nil
		case k == KeyCtrlC:
			e.render(prompt, line, len(line))
			io.WriteString(e.Out, "^C\r\n")
			e.cursorRow = 0
			return "", ErrInterrupt
		case k == KeyCtrlD && len(line) == 0:
			io.WriteString(e.Out, "\r\n")
			e.cursorRow = 0
			return "", io.EOF
		case k == KeyCtrlD || k == KeyDelete:
			if pos < len(line) {
				line = append(line[:pos], line[pos+1:]...)
			}
		case k == KeyBackspace:
			if pos > 0 {
				line = append(line[:pos-1], line[pos:]...)
				pos--
			}
		case k == KeyLeft:
			if pos > 0 {
				pos--
			}
		case k == KeyRight:
			if pos < len(line) {
				pos++
			}
		case k == KeyHome || k == KeyCtrlA:
			pos = 0
		case k == KeyEnd || k == KeyCtrlE:
			pos = len(line)
		case k == KeyCtrlU:
			line = append([]rune(nil), line[pos:]...)
			pos = 0
		case k == KeyCtrlK:
			line = line[:pos]
		case k == KeyCtrlW:
			start := pos
			for start > 0 && unicode.IsSpace(line[start-1]) {
				start--
			}
			for start > 0 && !unicode.IsSpace(line[start-1]) {
				start--
			}
			line = append(line[:start], line[pos:]...)
			pos = start
		case k == KeyCtrlL:
			if e.Clear != nil {
				e.Clear()
			} else {
				io.WriteString(e.Out, "\x1b[H\x1b[2J")
			}
			e.cursorRow = 0
		case k == KeyUp:
			if histIdx == 0 {
				continue
			}
			if histIdx == len(e.history) {
				draft = append([]rune(nil), line...)
			}
			histIdx--
			line = []rune(e.history[histIdx])
			pos = len(line)
		case k == KeyDown:
			if histIdx >= len(e.history) {
				continue
			}
			histIdx++
			if histIdx == len(e.history) {
				line = append([]rune(nil), draft...)
			} else {
				line = []rune(e.history[histIdx])
			}
			pos = len(line)
		case k == KeyTab:
			if e.Complete == nil {
				continue
			}
			newLine, newPos, choices := e.Complete(append([]rune(nil), line...), pos)
			if len(newLine) <= maxLen {
				line, pos = newLine, min(max(newPos, 0), len(newLine))
			}
			if len(choices) > 0 {
				e.render(prompt, line, len(line))
				fmt.Fprintf(e.Out, "\r\n%s\r\n", strings.Join(choices, "  "))
				e.cursorRow = 0
			}
		case k.Printable():
			if len(line) >= maxLen {
				io.WriteString(e.Out, "\a")
				continue
			}
			line = append(line, 0)
			copy(line[pos+1:], line[pos:])
			line[pos] = rune(k)
			pos++
		default:
			continue
		}
		e.render(prompt, line, pos)
	}
}

// render redraws prompt+line from the prompt's first row and leaves the
// cursor at pos. It handles the terminal's pending-wrap state when the text
// ends exactly at the right margin.
func (e *Editor) render(prompt string, line []rune, pos int) {
	w := e.width()
	var b strings.Builder
	if e.cursorRow > 0 {
		fmt.Fprintf(&b, "\x1b[%dA", e.cursorRow)
	}
	b.WriteByte('\r')
	b.WriteString(prompt)
	b.WriteString(string(line))
	plen := VisibleLen(prompt)
	end := plen + len(line)
	if end > 0 && end%w == 0 {
		b.WriteString("\r\n")
	}
	b.WriteString("\x1b[J")
	target := plen + pos
	endRow, tRow, tCol := end/w, target/w, target%w
	if endRow > tRow {
		fmt.Fprintf(&b, "\x1b[%dA", endRow-tRow)
	}
	b.WriteByte('\r')
	if tCol > 0 {
		fmt.Fprintf(&b, "\x1b[%dC", tCol)
	}
	e.cursorRow = tRow
	io.WriteString(e.Out, b.String())
}

// VisibleLen is the number of columns s occupies, ignoring CSI sequences.
func VisibleLen(s string) int {
	n := 0
	inEsc, inCSI := false, false
	for _, r := range s {
		switch {
		case inCSI:
			if r >= 0x40 && r <= 0x7e {
				inCSI = false
			}
		case inEsc:
			inEsc = false
			inCSI = r == '['
		case r == 0x1b:
			inEsc = true
		case r >= 0x20:
			n++
		}
	}
	return n
}
