use crate::{
    md::{self, Doc, Link, Seg},
    vault::{Target, Vault},
};
use ratatui::{
    Frame,
    crossterm::event::{self, Event, KeyCode, KeyEventKind},
    layout::{Constraint, Layout, Margin, Rect},
    style::{Style, Stylize},
    text::{Line, Span},
    widgets::{List, ListState, Paragraph},
};

struct Reader {
    note: usize,
    doc: Doc,
    wrapped: Vec<Vec<Seg>>,
    width: usize,
    height: usize,
    scroll: usize,
    focus: Option<usize>,
}

impl Reader {
    fn new(v: &Vault, note: usize) -> Self {
        let backlinks = v.backlinks[note].iter().map(|&i| (i, v.notes[i].title.as_str()));
        let doc = md::render(&v.read(note), backlinks);
        Reader { note, doc, wrapped: vec![], width: 0, height: 0, scroll: 0, focus: None }
    }

    fn layout(&mut self, width: usize, height: usize) {
        if width != self.width {
            self.wrapped = md::wrap(&self.doc.lines, width);
            self.width = width;
        }
        self.height = height;
    }

    fn scroll_by(&mut self, d: isize) {
        let max = self.wrapped.len().saturating_sub(self.height);
        self.scroll = self.scroll.saturating_add_signed(d).min(max);
    }

    fn cycle(&mut self, forward: bool) {
        let n = self.doc.links.len();
        if n == 0 {
            return;
        }
        let f = match (self.focus, forward) {
            (None, true) => 0,
            (None, false) => n - 1,
            (Some(f), true) => (f + 1) % n,
            (Some(f), false) => (f + n - 1) % n,
        };
        self.focus = Some(f);
        if let Some(y) = self.wrapped.iter().position(|l| l.iter().any(|s| s.link == Some(f)))
            && (y < self.scroll || y >= self.scroll + self.height)
        {
            self.scroll = y.saturating_sub(self.height / 3);
        }
    }

    /// Handles reader-local keys; returns true when the reader should close.
    fn key(&mut self, code: KeyCode) -> bool {
        let page = self.height as isize;
        match code {
            KeyCode::Esc | KeyCode::Char('q') => return true,
            KeyCode::Char('j') | KeyCode::Down => self.scroll_by(1),
            KeyCode::Char('k') | KeyCode::Up => self.scroll_by(-1),
            KeyCode::Char(' ') | KeyCode::PageDown => self.scroll_by(page),
            KeyCode::PageUp => self.scroll_by(-page),
            KeyCode::Char('g') => self.scroll = 0,
            KeyCode::Char('G') => self.scroll_by(isize::MAX / 2),
            KeyCode::Tab => self.cycle(true),
            KeyCode::BackTab => self.cycle(false),
            _ => {}
        }
        false
    }

    fn draw(&mut self, f: &mut Frame, area: Rect) {
        let area = area.inner(Margin::new(1, 0));
        self.layout(area.width as usize, area.height as usize);
        let lines: Vec<Line> = self.wrapped[self.scroll.min(self.wrapped.len())..]
            .iter()
            .take(self.height)
            .map(|l| {
                l.iter()
                    .map(|s| {
                        let focused = s.link.is_some() && s.link == self.focus;
                        Span::styled(s.text.as_str(), if focused { s.style.reversed() } else { s.style })
                    })
                    .collect::<Line>()
            })
            .collect();
        f.render_widget(Paragraph::new(lines), area);
    }
}

pub struct App {
    vault: Vault,
    filter: String,
    filtering: bool,
    visible: Vec<usize>,
    list: ListState,
    reader: Option<Reader>,
    history: Vec<usize>,
    pos: usize,
    status: String,
    edit: Option<std::path::PathBuf>,
}

impl App {
    pub fn new(vault: Vault) -> Self {
        let mut app = App {
            vault,
            filter: String::new(),
            filtering: false,
            visible: vec![],
            list: ListState::default(),
            reader: None,
            history: vec![],
            pos: 0,
            status: String::new(),
            edit: None,
        };
        app.refilter();
        app
    }

    fn refilter(&mut self) {
        let words: Vec<String> = self.filter.to_lowercase().split_whitespace().map(String::from).collect();
        self.visible = (0..self.vault.notes.len())
            .filter(|&i| {
                let p = self.vault.notes[i].path.to_string_lossy().to_lowercase();
                words.iter().all(|w| p.contains(w.as_str()))
            })
            .collect();
        self.list.select((!self.visible.is_empty()).then_some(0));
    }

    fn current(&self) -> Option<usize> {
        self.reader.as_ref().map(|r| r.note).or_else(|| self.list.selected().and_then(|s| self.visible.get(s).copied()))
    }

    /// Reloads the vault from disk, remapping history, selection and reader position by path.
    fn reload(&mut self) {
        let old = &self.vault;
        let path = |i: usize| old.notes[i].path.clone();
        let hist: Vec<_> = self.history.iter().map(|&i| path(i)).collect();
        let sel = self.list.selected().and_then(|s| self.visible.get(s)).map(|&i| path(i));
        let reader = self.reader.as_ref().map(|r| (path(r.note), r.scroll, r.focus));

        self.vault = Vault::load(&self.vault.root);
        let find = |v: &Vault, p: &std::path::Path| v.notes.iter().position(|n| n.path == p);

        self.history = hist.iter().filter_map(|p| find(&self.vault, p)).collect();
        self.pos = self.pos.min(self.history.len().saturating_sub(1));
        self.refilter();
        if let Some(s) = sel.and_then(|p| find(&self.vault, &p)).and_then(|i| self.visible.iter().position(|&v| v == i)) {
            self.list.select(Some(s));
        }
        self.reader = reader.and_then(|(p, scroll, focus)| {
            let Some(i) = find(&self.vault, &p) else {
                self.status = format!("{} was removed", p.display());
                return None;
            };
            let mut r = Reader::new(&self.vault, i);
            r.scroll = scroll;
            r.focus = focus.filter(|&f| f < r.doc.links.len());
            Some(r)
        });
    }

    fn open(&mut self, note: usize) {
        self.history.truncate(self.pos + 1);
        self.history.push(note);
        self.pos = self.history.len() - 1;
        self.reader = Some(Reader::new(&self.vault, note));
    }

    fn go(&mut self, d: isize) {
        if let Some(p) = self.pos.checked_add_signed(d).filter(|&p| p < self.history.len()) {
            self.pos = p;
            self.reader = Some(Reader::new(&self.vault, self.history[p]));
        }
    }

    fn follow(&mut self) {
        let Some(r) = &self.reader else { return };
        let Some(f) = r.focus else { return };
        let target = match &r.doc.links[f] {
            Link::Note(i) => Target::Note(*i),
            Link::Name(n) => self.vault.target(n),
        };
        self.status = match target {
            Target::Note(i) => return self.open(i),
            Target::File(p) => external(p.as_os_str()),
            Target::Url(u) => external(u.as_ref()),
            Target::Missing => format!("unresolved: {}", self.link_name(f)),
        };
    }

    fn link_name(&self, f: usize) -> String {
        match self.reader.as_ref().map(|r| &r.doc.links[f]) {
            Some(Link::Name(n)) => n.clone(),
            _ => String::new(),
        }
    }

    /// Returns false when the app should quit.
    fn key(&mut self, code: KeyCode) -> bool {
        self.status.clear();
        if self.filtering {
            match code {
                KeyCode::Enter => self.filtering = false,
                KeyCode::Esc => {
                    self.filter.clear();
                    self.filtering = false;
                    self.refilter()
                }
                KeyCode::Backspace => {
                    self.filter.pop();
                    self.refilter()
                }
                KeyCode::Char(c) => {
                    self.filter.push(c);
                    self.refilter()
                }
                _ => {}
            }
            return true;
        }
        match code {
            KeyCode::Char('e') => self.edit = self.current().map(|i| self.vault.root.join(&self.vault.notes[i].path)),
            KeyCode::Char('[') => self.go(-1),
            KeyCode::Char(']') => self.go(1),
            KeyCode::Enter if self.reader.is_some() => self.follow(),
            KeyCode::Enter => {
                if let Some(&i) = self.list.selected().and_then(|s| self.visible.get(s)) {
                    self.open(i)
                }
            }
            c => match &mut self.reader {
                Some(r) => {
                    if r.key(c) {
                        self.reader = None
                    }
                }
                None => match c {
                    KeyCode::Char('q') => return false,
                    KeyCode::Char('j') | KeyCode::Down => self.list.select_next(),
                    KeyCode::Char('k') | KeyCode::Up => self.list.select_previous(),
                    KeyCode::Char('g') => self.list.select_first(),
                    KeyCode::Char('G') => self.list.select_last(),
                    KeyCode::Char('/') => self.filtering = true,
                    _ => {}
                },
            },
        }
        true
    }

    fn draw(&mut self, f: &mut Frame) {
        let [head, body, foot] = Layout::vertical([Constraint::Length(1), Constraint::Fill(1), Constraint::Length(1)]).areas(f.area());
        let crumb = match &self.reader {
            Some(r) => self.vault.notes[r.note].path.display().to_string(),
            None => format!("notes ({}/{})", self.visible.len(), self.vault.notes.len()),
        };
        f.render_widget(Line::from(vec![" kb ".black().on_cyan().bold(), format!(" {} › {crumb}", self.vault.name()).into()]), head);

        match &mut self.reader {
            Some(r) => r.draw(f, body),
            None => {
                let items = self.visible.iter().map(|&i| {
                    let n = &self.vault.notes[i];
                    let dir = n.path.parent().map(|p| p.display().to_string()).unwrap_or_default();
                    Line::from(vec![Span::raw(n.title.as_str()), format!("  {dir}").dark_gray()])
                });
                f.render_stateful_widget(List::new(items).highlight_style(Style::new().reversed()), body, &mut self.list);
            }
        }

        let footer = match (self.filtering, self.status.is_empty(), self.reader.is_some()) {
            (true, _, _) => Line::from(format!("/{}█", self.filter)),
            (_, false, _) => Line::from(self.status.as_str().yellow()),
            (_, _, true) => "↑↓ j/k scroll  tab/S-tab link  ⏎ follow  e edit  [ ] history  esc back".dark_gray().into(),
            (_, _, false) => "↑↓ j/k move  / filter  ⏎ open  e edit  [ ] history  q quit".dark_gray().into(),
        };
        f.render_widget(footer, foot);
    }
}

/// Hands a URL or file to the desktop opener without blocking or touching the terminal.
fn external(what: &std::ffi::OsStr) -> String {
    let opener = if cfg!(target_os = "macos") { "open" } else { "xdg-open" };
    let spawned = std::process::Command::new(opener)
        .arg(what)
        .stdin(std::process::Stdio::null())
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::null())
        .spawn();
    match spawned {
        Ok(_) => format!("opened {}", what.to_string_lossy()),
        Err(e) => format!("{opener} failed: {e}"),
    }
}

/// True when a changed path can affect notes or attachments (git and Obsidian internals can't).
fn relevant(p: &std::path::Path) -> bool {
    !p.components().any(|c| c.as_os_str() == ".git" || c.as_os_str() == ".obsidian")
}

/// Suspends the TUI, runs `$VISUAL`/`$EDITOR` (may contain args) on `path`, then restores it.
fn edit(term: &mut ratatui::DefaultTerminal, path: &std::path::Path) -> Option<String> {
    let editor = std::env::var("VISUAL").or_else(|_| std::env::var("EDITOR")).unwrap_or_else(|_| "vi".into());
    ratatui::restore();
    let status = std::process::Command::new("sh").arg("-c").arg(format!("{editor} \"$1\"")).arg("sh").arg(path).status();
    *term = ratatui::init();
    match status {
        Ok(s) if s.success() => None,
        Ok(s) => Some(format!("{editor} exited with {s}")),
        Err(e) => Some(format!("{editor}: {e}")),
    }
}

pub fn run(vault: Vault) -> std::io::Result<()> {
    use notify_debouncer_full::{DebounceEventResult, new_debouncer, notify::RecursiveMode};
    use std::time::Duration;

    let (tx, rx) = std::sync::mpsc::channel::<DebounceEventResult>();
    let mut watcher = new_debouncer(Duration::from_millis(200), None, tx).map_err(std::io::Error::other)?;
    watcher.watch(&vault.root, RecursiveMode::Recursive).map_err(std::io::Error::other)?;

    let mut app = App::new(vault);
    ratatui::run(|term| loop {
        term.draw(|f| app.draw(f))?;
        if let Some(path) = app.edit.take() {
            let err = edit(term, &path);
            app.reload();
            app.status = err.unwrap_or_default();
            continue;
        }
        if event::poll(Duration::from_millis(100))?
            && let Event::Key(k) = event::read()?
            && k.kind == KeyEventKind::Press
            && !app.key(k.code)
        {
            return Ok(());
        }
        let changed = rx.try_iter().flatten().flatten().any(|e| e.paths.iter().any(|p| relevant(p)));
        if changed {
            app.reload();
        }
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use ratatui::{Terminal, backend::TestBackend};

    fn app() -> (tempfile::TempDir, App) {
        let d = tempfile::tempdir().unwrap();
        std::fs::write(d.path().join("A.md"), "# A\n\nGo to [[B]].").unwrap();
        std::fs::write(d.path().join("B.md"), "# B\n\nMissing [[Nowhere]].").unwrap();
        let a = App::new(Vault::load(d.path()));
        (d, a)
    }

    fn screen(a: &mut App) -> String {
        let mut t = Terminal::new(TestBackend::new(40, 20)).unwrap();
        t.draw(|f| a.draw(f)).unwrap();
        t.backend().buffer().content().chunks(40).map(|r| r.iter().map(|c| c.symbol()).collect::<String>().trim_end().to_string()).collect::<Vec<_>>().join("\n")
    }

    fn keys(a: &mut App, ks: &[KeyCode]) {
        ks.iter().for_each(|&k| {
            a.key(k);
            screen(a);
        });
    }

    #[test]
    fn filter_open_follow_and_history() {
        let (_d, mut a) = app();
        assert!(screen(&mut a).contains("notes (2/2)"));
        keys(&mut a, &[KeyCode::Char('/'), KeyCode::Char('a'), KeyCode::Enter, KeyCode::Enter]);
        assert!(screen(&mut a).contains("› A.md"));
        keys(&mut a, &[KeyCode::Tab, KeyCode::Enter]);
        assert!(screen(&mut a).contains("› B.md"));
        keys(&mut a, &[KeyCode::Char('[')]);
        assert!(screen(&mut a).contains("› A.md"));
        keys(&mut a, &[KeyCode::Char(']')]);
        assert!(screen(&mut a).contains("← A"), "backlinks section missing");
    }

    #[test]
    fn unresolved_link_reports_status() {
        let (_d, mut a) = app();
        keys(&mut a, &[KeyCode::Char('j'), KeyCode::Enter, KeyCode::Tab, KeyCode::Enter]);
        assert!(screen(&mut a).contains("unresolved: Nowhere"));
    }

    #[test]
    fn arrows_move_like_jk() {
        let (_d, mut a) = app();
        keys(&mut a, &[KeyCode::Down, KeyCode::Enter]);
        assert!(screen(&mut a).contains("› B.md"));
        keys(&mut a, &[KeyCode::Esc, KeyCode::Up, KeyCode::Enter]);
        assert!(screen(&mut a).contains("› A.md"));
    }

    #[test]
    fn e_requests_editor_for_current_note() {
        let (d, mut a) = app();
        keys(&mut a, &[KeyCode::Char('e')]);
        assert_eq!(a.edit, Some(d.path().join("A.md")));
        keys(&mut a, &[KeyCode::Down, KeyCode::Enter, KeyCode::Char('e')]);
        assert_eq!(a.edit, Some(d.path().join("B.md")));
    }

    #[test]
    fn reload_keeps_reader_history_and_picks_up_changes() {
        let (d, mut a) = app();
        keys(&mut a, &[KeyCode::Enter, KeyCode::Tab, KeyCode::Enter]);
        std::fs::write(d.path().join("B.md"), "# B\n\nEdited in nvim.").unwrap();
        std::fs::write(d.path().join("0 New.md"), "new").unwrap();
        a.reload();
        let s = screen(&mut a);
        assert!(s.contains("› B.md") && s.contains("Edited in nvim."), "{s}");
        keys(&mut a, &[KeyCode::Char('[')]);
        assert!(screen(&mut a).contains("› A.md"));
        keys(&mut a, &[KeyCode::Esc]);
        assert!(screen(&mut a).contains("notes (3/3)"));
    }

    #[test]
    fn reload_closes_reader_of_deleted_note() {
        let (d, mut a) = app();
        keys(&mut a, &[KeyCode::Enter]);
        std::fs::remove_file(d.path().join("A.md")).unwrap();
        a.reload();
        let s = screen(&mut a);
        assert!(s.contains("A.md was removed") && s.contains("notes (1/1)"), "{s}");
    }

    #[test]
    fn ignores_git_and_obsidian_changes() {
        assert!(!relevant(std::path::Path::new("/v/.git/index")));
        assert!(!relevant(std::path::Path::new("/v/.obsidian/workspace.json")));
        assert!(relevant(std::path::Path::new("/v/Notes/x.md")));
    }

    #[test]
    fn quit_from_list() {
        let (_d, mut a) = app();
        assert!(!a.key(KeyCode::Char('q')));
    }
}
