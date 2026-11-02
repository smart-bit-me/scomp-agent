// Package term decodes terminal input bytes into keys and provides a small
// line editor. It never touches a real terminal: input arrives as bytes from
// the relay and output goes to an io.Writer.
package term

import "unicode/utf8"

// Key is a decoded keypress: a printable rune, a C0 control code (Ctrl-C is
// 3), or one of the special keys below.
type Key rune

// Special keys live above the Unicode range so they never collide with runes.
const (
	KeyUp Key = utf8.MaxRune + 1 + iota
	KeyDown
	KeyRight
	KeyLeft
	KeyHome
	KeyEnd
	KeyDelete
	KeyPageUp
	KeyPageDown
	KeyUnknown // an escape sequence we do not handle
)

// Control keys, by their byte value.
const (
	KeyCtrlA     Key = 0x01
	KeyCtrlC     Key = 0x03
	KeyCtrlD     Key = 0x04
	KeyCtrlE     Key = 0x05
	KeyBell      Key = 0x07
	KeyBackspace Key = 0x7f
	KeyTab       Key = 0x09
	KeyCtrlK     Key = 0x0b
	KeyCtrlL     Key = 0x0c
	KeyEnter     Key = 0x0d
	KeyCtrlU     Key = 0x15
	KeyCtrlW     Key = 0x17
	KeyEsc       Key = 0x1b
)

// Printable reports whether k inserts a character.
func (k Key) Printable() bool {
	return k >= 0x20 && k != KeyBackspace && k <= utf8.MaxRune
}

type decodeState int

const (
	stGround decodeState = iota
	stEsc
	stCSI
	stCSIDiscard // an over-long sequence: skip to its final byte
	stSS3
)

// maxCSI bounds an escape sequence's parameter bytes; a longer sequence is
// discarded whole and reported as one KeyUnknown.
const maxCSI = 16

// Decoder turns input bytes into keys. It keeps state across Feed calls, so
// an escape sequence or UTF-8 rune split between two relay messages still
// decodes. It is not safe for concurrent use.
type Decoder struct {
	state  decodeState
	params []byte
	utf    []byte
	lastCR bool
}

// Feed decodes p and returns the completed keys. An ESC at the very end of p
// is reported as KeyEsc: remote clients send whole sequences per message, and
// waiting for more bytes would make a lone Escape key feel stuck.
func (d *Decoder) Feed(p []byte) []Key {
	var out []Key
	for i, b := range p {
		switch d.state {
		case stEsc:
			switch b {
			case '[':
				d.state, d.params = stCSI, d.params[:0]
			case 'O':
				d.state = stSS3
			default:
				// Alt+key: report Esc, then the key itself.
				d.state = stGround
				out = append(out, KeyEsc)
				out = d.ground(out, b)
			}
			continue
		case stCSI:
			if b >= 0x40 && b <= 0x7e {
				d.state = stGround
				out = append(out, csiKey(d.params, b))
				continue
			}
			if len(d.params) >= maxCSI {
				d.state = stCSIDiscard
				continue
			}
			d.params = append(d.params, b)
			continue
		case stCSIDiscard:
			if b >= 0x40 && b <= 0x7e {
				d.state = stGround
				out = append(out, KeyUnknown)
			}
			continue
		case stSS3:
			d.state = stGround
			out = append(out, ss3Key(b))
			continue
		}
		if b == 0x1b {
			if i == len(p)-1 {
				out = append(out, KeyEsc)
				continue
			}
			d.state = stEsc
			continue
		}
		out = d.ground(out, b)
	}
	return out
}

func (d *Decoder) ground(out []Key, b byte) []Key {
	cr := d.lastCR
	d.lastCR = false
	switch {
	case len(d.utf) > 0 || b >= 0x80:
		d.utf = append(d.utf, b)
		if utf8.FullRune(d.utf) {
			r, _ := utf8.DecodeRune(d.utf)
			d.utf = d.utf[:0]
			if r != utf8.RuneError {
				out = append(out, Key(r))
			}
		} else if len(d.utf) >= utf8.UTFMax {
			d.utf = d.utf[:0]
		}
	case b == '\r':
		d.lastCR = true
		out = append(out, KeyEnter)
	case b == '\n':
		if !cr { // CR LF is one Enter
			out = append(out, KeyEnter)
		}
	case b == 0x08:
		out = append(out, KeyBackspace)
	default:
		out = append(out, Key(b))
	}
	return out
}

func csiKey(params []byte, final byte) Key {
	switch final {
	case 'A':
		return KeyUp
	case 'B':
		return KeyDown
	case 'C':
		return KeyRight
	case 'D':
		return KeyLeft
	case 'H':
		return KeyHome
	case 'F':
		return KeyEnd
	case '~':
		switch string(params) {
		case "1", "7":
			return KeyHome
		case "4", "8":
			return KeyEnd
		case "3":
			return KeyDelete
		case "5":
			return KeyPageUp
		case "6":
			return KeyPageDown
		}
	}
	return KeyUnknown
}

func ss3Key(b byte) Key {
	switch b {
	case 'A':
		return KeyUp
	case 'B':
		return KeyDown
	case 'C':
		return KeyRight
	case 'D':
		return KeyLeft
	case 'H':
		return KeyHome
	case 'F':
		return KeyEnd
	}
	return KeyUnknown
}
