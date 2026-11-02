package fakeshell

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// strftime formats t with GNU date's % directives (C locale). Unknown
// directives are printed literally.
func strftime(t time.Time, format string) string {
	var b strings.Builder
	for i := 0; i < len(format); i++ {
		c := format[i]
		if c != '%' || i == len(format)-1 {
			b.WriteByte(c)
			continue
		}
		i++
		d := format[i]
		if d == ':' && i+1 < len(format) && format[i+1] == 'z' {
			i++
			b.WriteString(t.Format("-07:00"))
			continue
		}
		switch d {
		case '%':
			b.WriteByte('%')
		case 'a':
			b.WriteString(t.Format("Mon"))
		case 'A':
			b.WriteString(t.Format("Monday"))
		case 'b', 'h':
			b.WriteString(t.Format("Jan"))
		case 'B':
			b.WriteString(t.Format("January"))
		case 'c':
			b.WriteString(t.Format("Mon Jan _2 15:04:05 2006"))
		case 'C':
			fmt.Fprintf(&b, "%02d", t.Year()/100)
		case 'd':
			fmt.Fprintf(&b, "%02d", t.Day())
		case 'D':
			b.WriteString(t.Format("01/02/06"))
		case 'e':
			fmt.Fprintf(&b, "%2d", t.Day())
		case 'F':
			b.WriteString(t.Format("2006-01-02"))
		case 'H':
			fmt.Fprintf(&b, "%02d", t.Hour())
		case 'I':
			fmt.Fprintf(&b, "%02d", hour12(t))
		case 'j':
			fmt.Fprintf(&b, "%03d", t.YearDay())
		case 'k':
			fmt.Fprintf(&b, "%2d", t.Hour())
		case 'l':
			fmt.Fprintf(&b, "%2d", hour12(t))
		case 'm':
			fmt.Fprintf(&b, "%02d", int(t.Month()))
		case 'M':
			fmt.Fprintf(&b, "%02d", t.Minute())
		case 'n':
			b.WriteByte('\n')
		case 'N':
			fmt.Fprintf(&b, "%09d", t.Nanosecond())
		case 'p':
			b.WriteString(t.Format("PM"))
		case 'P':
			b.WriteString(strings.ToLower(t.Format("PM")))
		case 'r':
			b.WriteString(t.Format("03:04:05 PM"))
		case 'R':
			b.WriteString(t.Format("15:04"))
		case 's':
			b.WriteString(strconv.FormatInt(t.Unix(), 10))
		case 'S':
			fmt.Fprintf(&b, "%02d", t.Second())
		case 't':
			b.WriteByte('\t')
		case 'T', 'X':
			b.WriteString(t.Format("15:04:05"))
		case 'u':
			wd := int(t.Weekday())
			if wd == 0 {
				wd = 7
			}
			b.WriteString(strconv.Itoa(wd))
		case 'w':
			b.WriteString(strconv.Itoa(int(t.Weekday())))
		case 'x':
			b.WriteString(t.Format("01/02/06"))
		case 'y':
			fmt.Fprintf(&b, "%02d", t.Year()%100)
		case 'Y':
			b.WriteString(strconv.Itoa(t.Year()))
		case 'z':
			b.WriteString(t.Format("-0700"))
		case 'Z':
			b.WriteString(t.Format("MST"))
		default:
			b.WriteByte('%')
			b.WriteByte(d)
		}
	}
	return b.String()
}

func hour12(t time.Time) int {
	h := t.Hour() % 12
	if h == 0 {
		h = 12
	}
	return h
}

// defaultDateFormat is GNU date's output in the C locale.
const defaultDateFormat = "%a %b %e %H:%M:%S %Z %Y"

// dateCommand implements date [-u] [-R | -I[spec]] [+FORMAT].
func dateCommand(now time.Time, args []string) (string, error) {
	format := defaultDateFormat
	for _, a := range args {
		switch {
		case a == "-u" || a == "--utc" || a == "--universal":
			now = now.UTC()
		case a == "-R" || a == "--rfc-email":
			format = "%a, %d %b %Y %H:%M:%S %z"
		case a == "-I" || a == "--iso-8601" || a == "-Idate" || a == "--iso-8601=date":
			format = "%F"
		case a == "-Ihours" || a == "--iso-8601=hours":
			format = "%FT%H%:z"
		case a == "-Iminutes" || a == "--iso-8601=minutes":
			format = "%FT%H:%M%:z"
		case a == "-Iseconds" || a == "--iso-8601=seconds":
			format = "%FT%T%:z"
		case a == "-s" || strings.HasPrefix(a, "--set"):
			return "", fmt.Errorf("date: cannot set date: Operation not permitted")
		case strings.HasPrefix(a, "+"):
			format = a[1:]
		case a == "-d" || strings.HasPrefix(a, "--date"):
			return "", fmt.Errorf("date: parsing other dates is not supported on this demo machine")
		default:
			return "", fmt.Errorf("date: invalid date '%s'", a)
		}
	}
	return strftime(now, format) + "\n", nil
}

// formatUptime renders an uptime like "3 days,  4:12" (uptime and top).
func formatUptime(d time.Duration) string {
	days := int(d.Hours()) / 24
	h := int(d.Hours()) % 24
	m := int(d.Minutes()) % 60
	var b strings.Builder
	switch days {
	case 0:
	case 1:
		b.WriteString("1 day, ")
	default:
		fmt.Fprintf(&b, "%d days, ", days)
	}
	if h == 0 && days == 0 {
		fmt.Fprintf(&b, "%d min", m)
	} else {
		fmt.Fprintf(&b, "%2d:%02d", h, m)
	}
	return b.String()
}

// formatUptimePretty renders uptime -p: "up 3 days, 4 hours, 12 minutes".
func formatUptimePretty(d time.Duration) string {
	days := int(d.Hours()) / 24
	h := int(d.Hours()) % 24
	m := int(d.Minutes()) % 60
	var parts []string
	unit := func(n int, s string) {
		if n == 1 {
			parts = append(parts, "1 "+s)
		} else if n > 1 {
			parts = append(parts, fmt.Sprintf("%d %ss", n, s))
		}
	}
	unit(days, "day")
	unit(h, "hour")
	unit(m, "minute")
	if len(parts) == 0 {
		return "up 0 minutes"
	}
	return "up " + strings.Join(parts, ", ")
}
