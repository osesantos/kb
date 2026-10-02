package tui

import (
	"fmt"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/osesantos/kb/internal/daily"
)

// calView lists the days of a month; days with a daily note are highlighted and the preview shows that note.
type calView struct {
	daily  daily.Daily
	month  time.Time
	today  time.Time
	selIdx int
}

func (m *Model) openCal() {
	now := time.Now()
	c := &calView{daily: daily.Resolve(m.v.Root, m.cfg.Daily(m.v.Root)), today: now}
	c.setMonth(now)
	c.selIdx = now.Day() - 1
	m.goTo(c)
}

func (c *calView) setMonth(t time.Time) {
	c.month = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
}

func (c *calView) days() int { return c.month.AddDate(0, 1, -1).Day() }

func (c *calView) date() time.Time { return c.month.AddDate(0, 0, c.selIdx) }

func (c *calView) crumb(*Model) string { return "cal" }
func (c *calView) listTitle() string   { return c.month.Format("January 2006") }
func (c *calView) sel() int            { return c.selIdx }

func (c *calView) rows(m *Model) []row {
	rows := make([]row, c.days())
	for i := range rows {
		d := c.month.AddDate(0, 0, i)
		mark := " "
		if d.YearDay() == c.today.YearDay() && d.Year() == c.today.Year() {
			mark = m.st.Glyphs.Today
		}
		text := fmt.Sprintf("%s %s %02d", mark, d.Format("Mon"), d.Day())
		if _, ok := m.v.Find(c.daily.Path(d)); ok {
			rows[i] = row{text: text, aux: "note", styled: m.st.Group.Header.Render(text)}
		} else {
			rows[i] = row{text: text, styled: m.st.ListRow.Aux.Render(text)}
		}
	}
	return rows
}

func (c *calView) preview(m *Model) (string, string, func(int) string) {
	d := c.date()
	path := c.daily.Path(d)
	title := d.Format("Monday, 2 January 2006")
	if i, ok := m.v.Find(path); ok {
		return title, path, func(w int) string { return m.renderNote(i, w) }
	}
	return title, "missing|" + path, func(int) string {
		return m.st.Empty.Title.Render("No daily note yet") + "\n\n" + m.st.Empty.Hint.Render(path+"\n\ne creates it")
	}
}

func (c *calView) key(m *Model, k string) tea.Cmd {
	if m.right {
		m.previewKey(k)
		return nil
	}
	if m.moveSel(&c.selIdx, c.days(), k) {
		return nil
	}
	path := c.daily.Path(c.date())
	switch k {
	case "H":
		c.setMonth(c.month.AddDate(0, -1, 0))
		c.selIdx = min(c.selIdx, c.days()-1)
	case "L":
		c.setMonth(c.month.AddDate(0, 1, 0))
		c.selIdx = min(c.selIdx, c.days()-1)
	case "t":
		c.setMonth(c.today)
		c.selIdx = c.today.Day() - 1
	case "esc", "q":
		m.backToNotes()
	case "enter":
		if i, ok := m.v.Find(path); ok {
			m.returnTo = c
			m.open(i)
		} else {
			m.info("no daily note %s  (e creates it)", path)
		}
	case "e":
		return m.editCmd(filepath.Join(m.v.Root, filepath.FromSlash(path)))
	}
	return nil
}

func (c *calView) help(right bool) []hint {
	if right {
		return []hint{{"j/k", "scroll"}, {"esc", "back"}}
	}
	return []hint{{"j/k", "day"}, {"H/L", "month"}, {"t", "today"}, {"⏎", "open"}, {"e", "edit/create"}, {"esc", "notes"}}
}
