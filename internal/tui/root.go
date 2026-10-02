// Package tui is kb's Bubble Tea interface: a titlebar, a list pane, a preview pane and a help bar.
package tui

import (
	"cmp"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/osesantos/kb/internal/config"
	"github.com/osesantos/kb/internal/git"
	"github.com/osesantos/kb/internal/tui/components"
	"github.com/osesantos/kb/internal/tui/styles"
	"github.com/osesantos/kb/internal/vault"
)

const (
	minWidth, minHeight = 60, 12
	listPercent         = 35
	// The notes view's left column (tree + board) is notesPercent of the width (at least notesMinWidth) plus notesExtra columns.
	notesPercent, notesMinWidth, notesExtra = 18, 24, 20
)

// row is one list line; styled, when set, replaces text on unselected rows.
type row struct{ text, aux, styled string }

// reloader is implemented by views that must re-derive their state after the vault reloads;
// beforeReload runs against the old vault and returns the restore step for the new one.
type reloader interface {
	beforeReload(m *Model) (restore func(m *Model))
}

// refresher is implemented by views backed by the working tree's git state, refreshed on any filesystem event.
type refresher interface{ refresh(m *Model) }

// commitWatcher is implemented by views backed by git history, refreshed only when .git changes.
type commitWatcher interface{ onCommit(m *Model) }

type hint struct{ key, desc string }

// view is one resource (notes, search, git...): a list on the left and a preview of the selection on the right.
type view interface {
	crumb(m *Model) string
	listTitle() string
	// list renders the left pane's content at the given inner size.
	list(m *Model, w, h int) string
	// preview returns the right pane's title, a cache key for its body, and a renderer for the body at a width.
	preview(m *Model) (title, key string, body func(width int) string)
	key(m *Model, k string) tea.Cmd
	help(focusRight bool) []hint
}

type prompt struct {
	label string
	text  string
	done  func(m *Model, text string) tea.Cmd
	// live, when set, runs on every edit (the `/` filter).
	live func(m *Model, text string)
}

// Model is the root Bubble Tea model.
type Model struct {
	st    *styles.Styles
	cfg   config.Config
	v     *vault.Vault
	w, h  int
	view  view
	right bool
	// scroll is the preview pane's first visible line.
	scroll int
	// gen counts vault reloads, so cached previews never outlive the text they were built from.
	gen   int
	cache previewCache
	md    styles.Markdown

	prompt    *prompt
	picker    *picker
	form      *form
	finder    *finder
	cfgPath   string
	status    string
	statusErr bool
	dirty     int
	dirtyOK   bool
	hist      []string
	pos       int
	watch     *vault.Watcher
	// editing is the path last handed to $EDITOR.
	editing string
	// returnTo is the view Esc goes back to after opening a hit from it (search results).
	returnTo view
	find     find
	board    board
	pushing  bool
	// rw and rh are the preview pane's inner size from the last layout.
	rw, rh int
}

// find is the in-note search: preview lines containing the query and which one is current.
type find struct {
	query string
	hits  []int
	cur   int
}

type previewCache struct {
	key   string
	width int
	lines []string
}

// New builds the root model for a vault; watch may be nil (tests).
func New(cfg config.Config, v *vault.Vault, watch *vault.Watcher) *Model {
	m := &Model{st: styles.NewWithTheme(cfg.Theme), cfg: cfg, cfgPath: config.Path(), v: v, watch: watch, pos: -1, board: newBoard(time.Now())}
	m.view = newNotes(m)
	if watch != nil && len(watch.Unwatched) > 0 {
		m.fail("live refresh is off for %d folders (watch limit?)", len(watch.Unwatched))
	}
	return m
}

type (
	fsMsg       []string
	reloadedMsg struct{ v *vault.Vault }
	dirtyMsg    struct {
		root string
		n    int
		ok   bool
	}
	editedMsg struct{ err error }
	pushedMsg struct {
		out string
		err error
	}
	switchedMsg struct {
		name string
		v    *vault.Vault
		w    *vault.Watcher
		err  error
	}
)

func (m *Model) Init() tea.Cmd { return tea.Batch(m.waitFS(), m.refreshDirty(), m.recount()) }

func (m *Model) waitFS() tea.Cmd {
	if m.watch == nil {
		return nil
	}
	ch := m.watch.Batches
	return func() tea.Msg {
		b, ok := <-ch
		if !ok {
			return nil
		}
		return fsMsg(b)
	}
}

func (m *Model) refreshDirty() tea.Cmd {
	root := m.v.Root
	return func() tea.Msg {
		st, err := git.Status(root)
		return dirtyMsg{root: root, n: len(st), ok: err == nil}
	}
}

func (m *Model) reload() tea.Cmd {
	root := m.v.Root
	return func() tea.Msg { return reloadedMsg{vault.Load(root)} }
}

func (m *Model) info(format string, a ...any) {
	m.status, m.statusErr = fmt.Sprintf(format, a...), false
}
func (m *Model) fail(format string, a ...any) {
	m.status, m.statusErr = fmt.Sprintf(format, a...), true
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case dirtyMsg:
		if msg.root == m.v.Root {
			m.dirty, m.dirtyOK = msg.n, msg.ok
		}
	case fsMsg:
		cmds := []tea.Cmd{m.waitFS()}
		for _, p := range msg {
			if vault.Relevant(p) {
				cmds = append(cmds, m.reload())
				break
			}
		}
		if r, ok := m.view.(refresher); ok {
			r.refresh(m)
		}
		if c, ok := m.view.(commitWatcher); ok && slices.ContainsFunc(msg, vault.InGit) {
			c.onCommit(m)
		}
		if slices.ContainsFunc(msg, vault.InGit) {
			cmds = append(cmds, m.recount())
		}
		return m, tea.Batch(append(cmds, m.refreshDirty())...)
	case boardMsg:
		m.applyBoard(msg)
	case pushedMsg:
		m.pushing = false
		if msg.err != nil {
			m.fail("push failed: %v", msg.err)
		} else {
			m.info("pushed %s", strings.TrimSpace(msg.out))
		}
		return m, m.refreshDirty()
	case switchedMsg:
		if msg.err != nil {
			m.fail("switch failed: %v", msg.err)
			return m, nil
		}
		if m.watch != nil {
			_ = m.watch.Close() // the old watcher is discarded either way
		}
		m.v, m.watch, m.hist, m.pos, m.gen = msg.v, msg.w, nil, -1, m.gen+1
		m.picker, m.prompt, m.form, m.finder, m.board = nil, nil, nil, nil, newBoard(time.Now())
		m.goTo(newNotes(m))
		m.info("switched to %s", msg.name)
		if n := len(msg.w.Unwatched); n > 0 {
			m.fail("switched to %s; live refresh is off for %d folders", msg.name, n)
		}
		return m, tea.Batch(m.waitFS(), m.refreshDirty(), m.recount())
	case reloadedMsg:
		if msg.v.Root == m.v.Root {
			m.applyReload(msg.v)
			return m, m.recount()
		}
	case editedMsg:
		if msg.err != nil {
			m.fail("editor: %v", msg.err)
		}
		return m, tea.Batch(m.reload(), m.refreshDirty())
	case tea.PasteMsg:
		switch {
		case m.finder != nil:
			m.finderPaste(msg.Content)
		case m.form != nil:
			m.formPaste(msg.Content)
		case m.prompt != nil:
			m.prompt.text += strings.NewReplacer("\r", "", "\n", " ").Replace(msg.Content)
		}
	case tea.KeyPressMsg:
		return m, m.keyPress(msg.String())
	}
	return m, nil
}

func (m *Model) keyPress(k string) tea.Cmd {
	m.status = ""
	switch {
	case k == "ctrl+c":
		return tea.Quit
	case m.finder != nil:
		return m.finderKey(k)
	case m.form != nil:
		return m.formKey(k)
	case m.prompt != nil:
		return m.promptKey(k)
	case m.picker != nil:
		return m.pickerKey(k)
	case k == ":":
		m.prompt = &prompt{label: ":", done: runCommand}
		return nil
	}
	return m.view.key(m, k)
}

func (m *Model) promptKey(k string) tea.Cmd {
	p := m.prompt
	switch k {
	case "esc":
		m.prompt = nil
		if p.live != nil {
			p.live(m, "")
		}
	case "enter":
		m.prompt = nil
		return p.done(m, strings.TrimSpace(p.text))
	case "backspace":
		if r := []rune(p.text); len(r) > 0 {
			p.text = string(r[:len(r)-1])
		}
	case "space":
		p.text += " "
	default:
		if r := []rune(k); len(r) == 1 {
			p.text += k
		}
	}
	if m.prompt != nil && p.live != nil {
		p.live(m, p.text)
	}
	return nil
}

// runCommand is the `:` resource switch, like k9s.
func runCommand(m *Model, c string) tea.Cmd {
	name, arg, _ := strings.Cut(c, " ")
	switch name {
	case "notes", "n":
		m.goTo(newNotes(m))
	case "search", "s":
		if arg == "" {
			m.openFinder()
		} else {
			m.search(strings.TrimSpace(arg))
		}
	case "git", "g":
		m.openGit()
	case "activity", "a":
		m.openActivity()
	case "cal", "c":
		m.openCal()
	case "vaults", "v":
		m.goTo(newVaults(m))
	case "q", "quit":
		return tea.Quit
	case "":
	default:
		m.fail("unknown command: %s  (notes, search, git, activity, cal, vaults, quit)", name)
	}
	return nil
}

// goTo replaces the current view, dropping any pending return and in-note find.
func (m *Model) goTo(v view) {
	m.view, m.right, m.scroll, m.returnTo, m.find = v, false, 0, nil, find{}
}

// applyReload swaps in a freshly loaded vault and remaps every note index the UI holds: the current view,
// the view Esc returns to, the open link picker, the search popup and the history.
func (m *Model) applyReload(nv *vault.Vault) {
	restores := []func(*Model){}
	for _, v := range []view{m.view, m.returnTo} {
		if r, ok := v.(reloader); ok {
			restores = append(restores, r.beforeReload(m))
		}
	}
	pickSel := -1
	if m.picker != nil {
		pickSel = m.picker.sel
	}
	m.v = nv
	m.gen++
	for _, restore := range restores {
		restore(m)
	}
	if f := m.finder; f != nil {
		sel := f.sel
		f.rerun(m)
		f.sel = min(sel, max(len(f.hits)-1, 0))
	}
	m.picker = nil
	if n, ok := m.view.(*notesView); ok && pickSel >= 0 {
		if cur, ok := n.current(); ok {
			m.picker = newPicker(m, cur)
			m.picker.sel = min(pickSel, max(len(m.picker.items)-1, 0))
		}
	}
	keep, pos := m.hist[:0:0], -1
	for i, p := range m.hist {
		if _, ok := nv.Find(p); ok {
			keep = append(keep, p)
			if i <= m.pos {
				pos = len(keep) - 1
			}
		}
	}
	m.hist, m.pos = keep, pos
}

// open shows a note in the preview pane, focuses it and records it in the history.
func (m *Model) open(idx int) {
	p := m.v.Notes[idx].Path
	m.hist = append(m.hist[:m.pos+1], p)
	m.pos = len(m.hist) - 1
	m.show(idx)
}

// show selects a note and focuses the preview pane without touching the history.
func (m *Model) show(idx int) {
	if _, ok := m.view.(*notesView); !ok {
		m.view = newNotes(m)
	}
	m.view.(*notesView).selectNote(m, idx)
	m.right, m.scroll, m.find = true, 0, find{}
}

func (m *Model) back(d int) {
	p := m.pos + d
	if p < 0 || p >= len(m.hist) {
		return
	}
	if i, ok := m.v.Find(m.hist[p]); ok {
		m.pos = p
		m.show(i)
	}
}

func (m *Model) editCmd(path string) tea.Cmd {
	editor := cmp.Or(os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vi")
	if err := ensureDir(path); err != nil {
		m.fail("%v", err)
		return nil
	}
	m.editing = path
	return tea.ExecProcess(exec.Command("sh", "-c", editor+` "$1"`, "sh", path), func(err error) tea.Msg { return editedMsg{err} })
}

// external hands a URL or file to the desktop opener without touching the terminal.
func (m *Model) external(what string) {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	if err := exec.Command(opener, what).Start(); err != nil {
		m.fail("%s failed: %v", opener, err)
		return
	}
	m.info("opened %s", what)
}

// previewLines renders the selection's preview once per (content, width) and caches the lines.
func (m *Model) previewLines(width int) (string, []string) {
	title, key, body := m.view.preview(m)
	key = fmt.Sprintf("%d|%s", m.gen, key)
	if m.cache.key != key || m.cache.width != width {
		m.cache = previewCache{key: key, width: width, lines: strings.Split(body(width), "\n")}
	}
	return title, m.cache.lines
}

func (m *Model) View() tea.View {
	var out string
	switch {
	case m.w == 0:
		out = ""
	case m.w < minWidth || m.h < minHeight:
		msg := m.st.TooSmall.Render(fmt.Sprintf("Terminal too small. Minimum size: %dx%d.", minWidth, minHeight))
		out = lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, msg)
	case m.finder != nil:
		out = lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, m.finderView())
	case m.form != nil:
		out = lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, m.formView())
	case m.picker != nil:
		out = lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, m.pickerView())
	default:
		out = m.layout()
	}
	v := tea.NewView(out)
	v.AltScreen = true
	return v
}

func (m *Model) titlebar() string {
	dirty := ""
	if m.dirtyOK && m.dirty > 0 {
		dirty = fmt.Sprintf("  %s%d", m.st.Glyphs.Dirty, m.dirty)
	}
	left := m.st.TitleBar.Branding.Render("kb") + m.st.TitleBar.Subtext.Render(m.v.Name()+" › "+m.view.crumb(m)+dirty)
	pad := max(m.w-lipgloss.Width(left), 0)
	return left + m.st.TitleBar.Fill.Render(strings.Repeat(" ", pad))
}

func (m *Model) footer() string {
	switch {
	case m.prompt != nil:
		return m.st.Help.Bar.Width(m.w).Render(m.st.Prompt.Label.Render(m.prompt.label) + m.st.Prompt.Text.Render(m.prompt.text+"█"))
	case m.status != "":
		style := m.st.Status.Info
		if m.statusErr {
			style = m.st.Status.Error
		}
		return m.st.Help.Bar.Width(m.w).Render(style.Render(m.status))
	}
	if m.right && m.find.query != "" {
		n := len(m.find.hits)
		cur := 0
		if n > 0 {
			cur = m.find.cur + 1
		}
		return m.helpBar([]hint{{"n/N", "next/prev"}, {"esc", "clear"}, {"", fmt.Sprintf("match %d/%d for %q", cur, n, m.find.query)}})
	}
	return m.helpBar(m.view.help(m.right))
}

func (m *Model) helpBar(hints []hint) string {
	bar := m.st.Help.Bar.Width(m.w)
	avail := m.w - bar.GetHorizontalFrameSize()
	sep := m.st.Help.Separator.Render(" • ")
	var b strings.Builder
	used := 0
	for i, h := range hints {
		seg := m.st.Help.Key.Render(h.key) + m.st.Help.Description.Render(" "+h.desc)
		add := lipgloss.Width(seg)
		if i > 0 {
			add += lipgloss.Width(sep)
		}
		if used+add > avail {
			break
		}
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteString(seg)
		used += add
	}
	return bar.Render(b.String())
}

func (m *Model) layout() string {
	bodyH := max(m.h-2, 1)
	leftW := m.w * listPercent / 100
	if _, ok := m.view.(*notesView); ok {
		leftW = min(max(m.w*notesPercent/100, notesMinWidth)+notesExtra, m.w-minWidth/2)
	}
	rightW := m.w - leftW

	listH := bodyH
	_, notes := m.view.(*notesView)
	showBoard := notes && boardFits(bodyH)
	boardH := boardHeight
	if showBoard {
		listH = bodyH - boardH
	}
	listFocus := !m.right && !(showBoard && m.board.focus)
	lw, lh := components.TitledPanelInnerSize(m.st, listFocus, leftW, listH)
	left := components.PanelWithTitle(m.st, m.view.list(m, lw, lh), m.view.listTitle(), listFocus, leftW, listH).Content
	if showBoard {
		boardFocus := !m.right && m.board.focus
		bw, _ := components.TitledPanelInnerSize(m.st, boardFocus, leftW, boardH)
		left = lipgloss.JoinVertical(lipgloss.Left, left, components.PanelWithTitle(m.st, m.boardView(bw), m.boardTitle(), boardFocus, leftW, boardH).Content)
	}

	rw, rh := components.TitledPanelInnerSize(m.st, m.right, rightW, bodyH)
	m.rw, m.rh = rw, rh
	title, lines := m.previewLines(rw)
	m.scroll = max(min(m.scroll, len(lines)-rh), 0)
	end := min(m.scroll+rh, len(lines))
	shown := strings.Join(lines[m.scroll:end], "\n")
	right := components.PanelWithTitle(m.st, shown, title, m.right, rightW, bodyH).Content

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	return lipgloss.JoinVertical(lipgloss.Left, m.titlebar(), body, m.footer())
}

// listPane renders the visible window of the view's rows, keeping the selection in sight.
func (m *Model) listPane(w, h int, rows []row, sel int) string {
	if len(rows) == 0 {
		return components.CenteredContent(m.st, m.st.Empty.Hint.Render("nothing here"), w, h)
	}
	off := max(min(sel-h/2, len(rows)-h), 0)
	lines := []string{}
	for i := off; i < min(off+h, len(rows)); i++ {
		r := rows[i]
		text, aux := ansi.Truncate(r.text, max(w-1, 1), "…"), r.aux
		if r.styled != "" && i != sel {
			text = ansi.Truncate(r.styled, max(w-1, 1), "…")
		}
		if free := w - lipgloss.Width(text) - 2; aux != "" && free > 3 {
			aux = "  " + ansi.Truncate(aux, free, "…")
		} else {
			aux = ""
		}
		if i == sel {
			lines = append(lines, m.st.ListRow.Selected.Width(w).Render(text+m.st.ListRow.AuxSelected.Render(aux)))
		} else {
			lines = append(lines, m.st.ListRow.Normal.Render(text)+m.st.ListRow.Aux.Render(aux))
		}
	}
	return strings.Join(lines, "\n")
}
