package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/osesantos/kb/internal/daily"
	"github.com/osesantos/kb/internal/git"
)

// boardHeight is the board panel's total height: 7 weekday rows, the legend, the day line and the panel frame.
const boardHeight = 12

// board is the contribution grid under the notes tree: notes created per day of one month.
type board struct {
	month time.Time
	day   int
	// created lists the vault-relative notes created on each day of month, sorted.
	created map[int][]string
	loaded  bool
	focus   bool
}

type boardMsg struct {
	root    string
	month   time.Time
	created map[int][]string
}

func newBoard(now time.Time) board {
	return board{month: time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()), day: now.Day()}
}

func (b *board) days() int { return b.month.AddDate(0, 1, -1).Day() }

func (b *board) date() time.Time { return b.month.AddDate(0, 0, b.day-1) }

func (b *board) total() int {
	n := 0
	for _, f := range b.created {
		n += len(f)
	}
	return n
}

// shift moves the selected day by d days, crossing into the neighbouring month when needed.
func (b *board) shift(d int) (monthChanged bool) {
	t := b.date().AddDate(0, 0, d)
	changed := t.Month() != b.month.Month() || t.Year() != b.month.Year()
	if changed {
		b.month, b.loaded, b.created = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location()), false, nil
	}
	b.day = t.Day()
	return changed
}

// countCreated buckets notes created in month by day: git first-add dates, plus uncommitted new notes by mtime.
// A vault outside git falls back to every note's mtime.
func countCreated(root string, notes []string, month time.Time) map[int][]string {
	end := month.AddDate(0, 1, 0)
	byDay := map[int][]string{}
	add := func(path string, at time.Time) {
		if !at.Before(month) && at.Before(end) {
			byDay[at.Day()] = append(byDay[at.Day()], path)
		}
	}
	mtime := func(path string) (time.Time, bool) {
		st, err := os.Stat(filepath.Join(root, filepath.FromSlash(path)))
		return st.ModTime(), err == nil
	}
	created, err := git.Created(root, month, end)
	if err != nil {
		for _, p := range notes {
			if at, ok := mtime(p); ok {
				add(p, at)
			}
		}
		return byDay
	}
	for p, at := range created {
		add(p, at.In(month.Location()))
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		real = root
	}
	if top, ok := git.Toplevel(root); ok {
		st, _ := git.Status(root) // without a status, only committed notes are counted
		for _, e := range st {
			if (e.Untracked() || e.X == 'A') && strings.HasSuffix(e.Path, ".md") {
				rel, err := filepath.Rel(real, filepath.Join(top, e.Path))
				if at, ok := mtime(filepath.ToSlash(rel)); err == nil && ok {
					add(filepath.ToSlash(rel), at)
				}
			}
		}
	}
	for d := range byDay {
		slices.Sort(byDay[d])
		byDay[d] = slices.Compact(byDay[d])
	}
	return byDay
}

func (m *Model) recount() tea.Cmd {
	root, month := m.v.Root, m.board.month
	notes := make([]string, len(m.v.Notes))
	for i, n := range m.v.Notes {
		notes[i] = n.Path
	}
	return func() tea.Msg { return boardMsg{root: root, month: month, created: countCreated(root, notes, month)} }
}

func (m *Model) applyBoard(msg boardMsg) {
	if msg.root == m.v.Root && msg.month.Equal(m.board.month) {
		m.board.created, m.board.loaded = msg.created, true
	}
}

// level maps a day's count to a shade 0–4: 0 is empty, the busiest day of the month is 4, the rest split by quartile.
func level(n int, nonzero []int) int {
	if n == 0 || len(nonzero) == 0 {
		return 0
	}
	if n >= nonzero[len(nonzero)-1] {
		return 4
	}
	l := 1
	for _, q := range []int{nonzero[len(nonzero)/4], nonzero[len(nonzero)/2], nonzero[len(nonzero)*3/4]} {
		if n > q {
			l++
		}
	}
	return min(l, 4)
}

func (m *Model) boardTitle() string {
	b := &m.board
	if !b.loaded {
		return b.month.Format("January 2006") + " · …"
	}
	return fmt.Sprintf("%s · %d created", b.month.Format("January 2006"), b.total())
}

// boardView draws the month as weekday rows × week columns, GitHub style, with a legend and the selected day.
func (m *Model) boardView() string {
	b := &m.board
	st := m.st.Board
	nonzero := []int{}
	for _, f := range b.created {
		nonzero = append(nonzero, len(f))
	}
	slices.Sort(nonzero)
	lead := (int(b.month.Weekday()) + 6) % 7
	weeks := (lead + b.days() + 6) / 7
	labels := []string{"Mon", "", "Wed", "", "Fri", "", "Sun"}
	lines := make([]string, 0, 9)
	for wd := range 7 {
		var row strings.Builder
		row.WriteString(st.Label.Render(fmt.Sprintf("%-4s", labels[wd])))
		for w := range weeks {
			d := w*7 + wd - lead + 1
			switch {
			case d < 1 || d > b.days():
				row.WriteString("  ")
			case d == b.day && b.focus:
				row.WriteString(st.Selected.Render("■") + " ")
			default:
				row.WriteString(st.Shades[level(len(b.created[d]), nonzero)].Render("■") + " ")
			}
		}
		lines = append(lines, row.String())
	}
	legend := st.Label.Render("Less ")
	for _, s := range st.Shades {
		legend += s.Render("■") + " "
	}
	lines = append(lines, legend+st.Label.Render("More"))
	n := len(b.created[b.day])
	lines = append(lines, st.Label.Render(fmt.Sprintf("%s · %d created", b.date().Format("Mon 2 Jan"), n)))
	return strings.Join(lines, "\n")
}

func (m *Model) boardPreview() (string, string, func(int) string) {
	b := &m.board
	d := daily.Resolve(m.v.Root, m.cfg.Daily(m.v.Root))
	path := d.Path(b.date())
	title := b.date().Format("Monday, 2 January 2006")
	files := b.created[b.day]
	key := fmt.Sprintf("board|%s|%d", path, len(files))
	return title, key, func(w int) string {
		out := m.st.Group.Header.Render(fmt.Sprintf("%d notes created", len(files)))
		for _, f := range files {
			out += "\n  " + strings.TrimSuffix(f, ".md")
		}
		if i, ok := m.v.Find(path); ok {
			out += "\n\n" + m.renderNote(i, w)
		} else {
			out += "\n\n" + m.st.Empty.Hint.Render("no daily note "+path)
		}
		return out
	}
}

// boardKey handles keys while the board has focus; it reports whether the board consumed k.
func (m *Model) boardKey(k string) (tea.Cmd, bool) {
	b := &m.board
	move := func(d int) tea.Cmd {
		if b.shift(d) {
			return m.recount()
		}
		return nil
	}
	switch k {
	case "tab", "esc":
		b.focus = false
	case "j", "down":
		return move(1), true
	case "k", "up":
		return move(-1), true
	case "l", "right":
		return move(7), true
	case "h", "left":
		return move(-7), true
	case "L":
		return move(b.days() - b.day + 1), true
	case "H":
		return move(-b.day), true
	case "t":
		now := time.Now()
		changed := now.Month() != b.month.Month() || now.Year() != b.month.Year()
		*b = board{month: newBoard(now).month, day: now.Day(), created: b.created, loaded: b.loaded && !changed, focus: true}
		if changed {
			b.created = nil
			return m.recount(), true
		}
	case "enter":
		path := daily.Resolve(m.v.Root, m.cfg.Daily(m.v.Root)).Path(b.date())
		if i, ok := m.v.Find(path); ok {
			b.focus = false
			m.open(i)
		} else {
			m.info("no daily note %s", path)
		}
	case "e":
		b.focus = false
		return m.editCmd(filepath.Join(m.v.Root, filepath.FromSlash(daily.Resolve(m.v.Root, m.cfg.Daily(m.v.Root)).Path(b.date())))), true
	default:
		return nil, false
	}
	return nil, true
}

// boardFits reports whether the left pane is tall enough for the tree (at least 5 rows) plus the board.
func boardFits(bodyH int) bool { return bodyH-boardHeight >= 5+3 }
