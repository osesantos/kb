// Package daily resolves the daily-note convention: a folder plus a moment.js filename format, as Obsidian stores it.
package daily

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/osesantos/kb/internal/config"
)

// Daily is a vault's daily-note convention: the folder and the moment.js filename format.
type Daily struct {
	Folder string
	Format string
}

// Resolve takes each field from the config override, else .obsidian/daily-notes.json, else Obsidian's defaults.
func Resolve(root string, over config.DailyCfg) Daily {
	var obs struct{ Folder, Format string }
	if b, err := os.ReadFile(filepath.Join(root, ".obsidian", "daily-notes.json")); err == nil {
		_ = json.Unmarshal(b, &obs) // a broken file means no Obsidian settings, same as a missing one
	}
	return Daily{
		Folder: strings.Trim(cmp.Or(over.Folder, obs.Folder), "/"),
		Format: cmp.Or(over.Format, obs.Format, "YYYY-MM-DD"),
	}
}

// Path is the vault-relative, slash-separated path of the daily note for d.
func (d Daily) Path(t time.Time) string {
	return path.Join(d.Folder, Moment(d.Format, t)+".md")
}

var tokens = []string{"YYYY", "YY", "MMMM", "MMM", "MM", "M", "Do", "DD", "D", "dddd", "ddd", "WW", "W"}

func ordinal(n int) string {
	switch {
	case n%100 >= 11 && n%100 <= 13:
		return "th"
	case n%10 == 1:
		return "st"
	case n%10 == 2:
		return "nd"
	case n%10 == 3:
		return "rd"
	default:
		return "th"
	}
}

func token(tok string, t time.Time) string {
	_, week := t.ISOWeek()
	switch tok {
	case "YYYY":
		return t.Format("2006")
	case "YY":
		return t.Format("06")
	case "MMMM":
		return t.Format("January")
	case "MMM":
		return t.Format("Jan")
	case "MM":
		return t.Format("01")
	case "M":
		return strconv.Itoa(int(t.Month()))
	case "Do":
		return strconv.Itoa(t.Day()) + ordinal(t.Day())
	case "DD":
		return t.Format("02")
	case "D":
		return strconv.Itoa(t.Day())
	case "dddd":
		return t.Format("Monday")
	case "ddd":
		return t.Format("Mon")
	case "WW":
		return fmt.Sprintf("%02d", week)
	default:
		return strconv.Itoa(week)
	}
}

// Moment formats t with the moment.js tokens Obsidian users put in filenames; `[text]` is literal.
func Moment(format string, t time.Time) string {
	var out strings.Builder
	for rest := format; rest != ""; {
		if strings.HasPrefix(rest, "[") {
			if lit, tail, ok := strings.Cut(rest[1:], "]"); ok {
				out.WriteString(lit)
				rest = tail
				continue
			}
		}
		matched := false
		for _, tok := range tokens {
			if strings.HasPrefix(rest, tok) {
				out.WriteString(token(tok, t))
				rest = rest[len(tok):]
				matched = true
				break
			}
		}
		if !matched {
			_, size := utf8.DecodeRuneInString(rest)
			out.WriteString(rest[:size])
			rest = rest[size:]
		}
	}
	return out.String()
}
