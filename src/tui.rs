use crate::{
    git,
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

enum Prompt {
    Filter,
    Command(String),
    Commit(String),
}

/// The `:git` view: working-tree changes of the vault, with an optional diff pane.
struct GitView {
    top: std::path::PathBuf,
    entries: Vec<git::Entry>,
    list: ListState,
    diff: Option<(Vec<Line<'static>>, u16)>,
}

fn diff_lines(d: &str) -> Vec<Line<'static>> {
    d.lines()
        .map(|l| {
            let s = match l.as_bytes().first() {
                Some(b'+') if !l.starts_with("+++") => Style::new().green(),
                Some(b'-') if !l.starts_with("---") => Style::new().red(),
                Some(b'@') => Style::new().cyan(),
                _ if l.starts_with("diff ") || l.starts_with("index ") || l.starts_with("+++") || l.starts_with("---") => Style::new().dark_gray(),
                _ => Style::new(),
            };
            Line::styled(l.to_string(), s)
        })
        .collect()
}

pub struct App {
    vault: Vault,
    filter: String,
    prompt: Option<Prompt>,
    git: Option<GitView>,
    dirty: Option<usize>,
    push: Option<std::sync::mpsc::Receiver<Result<String, String>>>,
    quit: bool,
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
            prompt: None,
            git: None,
            dirty: None,
            push: None,
            quit: false,
            visible: vec![],
            list: ListState::default(),
            reader: None,
            history: vec![],
            pos: 0,
            status: String::new(),
            edit: None,
        };
        app.refilter();
        app.refresh_git();
        app
    }

    /// Re-reads git status for the header count and, when open, the git view (selection kept by path).
    fn refresh_git(&mut self) {
        let status = git::status(&self.vault.root);
        self.dirty = status.as_ref().ok().map(Vec::len);
        let (Some(g), Ok(entries)) = (&mut self.git, status) else { return };
        let sel = g.list.selected().and_then(|i| g.entries.get(i)).map(|e| e.path.clone());
        let idx = sel.and_then(|p| entries.iter().position(|e| e.path == p)).or((!entries.is_empty()).then_some(0));
        g.list.select(idx.map(|i| i.min(entries.len().saturating_sub(1))));
        g.entries = entries;
    }

    fn open_git(&mut self) {
        let Some(top) = git::toplevel(&self.vault.root) else {
            self.status = "not a git repository".into();
            return;
        };
        self.git = Some(GitView { top, entries: vec![], list: ListState::default(), diff: None });
        self.refresh_git();
    }

    fn git_result(&mut self, what: &str, r: Result<String, String>) {
        self.status = match r {
            Ok(out) if out.trim().is_empty() => what.into(),
            Ok(out) => format!("{what}: {}", out.trim()),
            Err(e) => format!("{what} failed: {e}"),
        };
        self.refresh_git();
    }

    fn git_key(&mut self, code: KeyCode) {
        let root = self.vault.root.clone();
        let Some(g) = &mut self.git else { return };
        if let Some((lines, scroll)) = &mut g.diff {
            match code {
                KeyCode::Esc | KeyCode::Char('q') => g.diff = None,
                KeyCode::Char('j') | KeyCode::Down => *scroll = scroll.saturating_add(1).min(lines.len() as u16),
                KeyCode::Char('k') | KeyCode::Up => *scroll = scroll.saturating_sub(1),
                KeyCode::Char(' ') | KeyCode::PageDown => *scroll = scroll.saturating_add(20).min(lines.len() as u16),
                KeyCode::PageUp => *scroll = scroll.saturating_sub(20),
                _ => {}
            }
            return;
        }
        let sel = g.list.selected().and_then(|i| g.entries.get(i)).cloned();
        match (code, sel) {
            (KeyCode::Esc | KeyCode::Char('q'), _) => self.git = None,
            (KeyCode::Char('j') | KeyCode::Down, _) => g.list.select_next(),
            (KeyCode::Char('k') | KeyCode::Up, _) => g.list.select_previous(),
            (KeyCode::Enter, Some(e)) => match git::diff(&root, &e) {
                Ok(d) => g.diff = Some((diff_lines(&d), 0)),
                Err(err) => self.status = err,
            },
            (KeyCode::Char(' '), Some(e)) if e.staged() => self.git_result("unstaged", git::unstage(&root, &e.path)),
            (KeyCode::Char(' '), Some(e)) => self.git_result("staged", git::stage(&root, &e.path)),
            (KeyCode::Char('a'), _) => self.git_result("staged all", git::stage_all(&root)),
            (KeyCode::Char('c'), _) => self.prompt = Some(Prompt::Commit(String::new())),
            (KeyCode::Char('p'), _) if self.push.is_none() => {
                let (tx, rx) = std::sync::mpsc::channel();
                std::thread::spawn(move || tx.send(git::push(&root)));
                self.push = Some(rx);
                self.status = "pushing…".into();
            }
            (KeyCode::Char('e'), Some(e)) => self.edit = Some(g.top.join(&e.path)),
            _ => {}
        }
    }

    /// Delivers a finished background push to the status bar; true if the screen should redraw.
    fn poll(&mut self) -> bool {
        let Some(r) = self.push.as_ref().and_then(|rx| rx.try_recv().ok()) else { return false };
        self.push = None;
        self.git_result("pushed", r);
        true
    }

    fn prompt_key(&mut self, code: KeyCode) {
        let Some(mut p) = self.prompt.take() else { return };
        let buf = match &mut p {
            Prompt::Filter => &mut self.filter,
            Prompt::Command(s) | Prompt::Commit(s) => s,
        };
        match code {
            KeyCode::Esc => {
                if matches!(p, Prompt::Filter) {
                    self.filter.clear();
                    self.refilter();
                }
                return;
            }
            KeyCode::Enter => return self.submit(p),
            KeyCode::Backspace => {
                buf.pop();
            }
            KeyCode::Char(c) => buf.push(c),
            _ => {}
        }
        if matches!(p, Prompt::Filter) {
            self.refilter();
        }
        self.prompt = Some(p);
    }

    fn submit(&mut self, p: Prompt) {
        match p {
            Prompt::Filter => {}
            Prompt::Command(c) => match c.trim() {
                "git" | "g" => self.open_git(),
                "notes" | "n" => {
                    self.git = None;
                    self.reader = None;
                }
                "q" | "quit" => self.quit = true,
                other => self.status = format!("unknown command: {other}  (git, notes, quit)"),
            },
            Prompt::Commit(m) if m.trim().is_empty() => self.status = "commit aborted: empty message".into(),
            Prompt::Commit(m) => {
                let r = git::commit(&self.vault.root, m.trim());
                self.git_result("committed", r)
            }
        }
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
        if self.prompt.is_some() {
            self.prompt_key(code);
            return !self.quit;
        }
        if code == KeyCode::Char(':') {
            self.prompt = Some(Prompt::Command(String::new()));
            return true;
        }
        if self.git.is_some() {
            self.git_key(code);
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
                    KeyCode::Char('/') => self.prompt = Some(Prompt::Filter),
                    _ => {}
                },
            },
        }
        true
    }

    fn draw(&mut self, f: &mut Frame) {
        let [head, body, foot] = Layout::vertical([Constraint::Length(1), Constraint::Fill(1), Constraint::Length(1)]).areas(f.area());
        let crumb = match (&self.git, &self.reader) {
            (Some(g), _) => {
                let sel = g.list.selected().and_then(|i| g.entries.get(i));
                match (&g.diff, sel) {
                    (Some(_), Some(e)) => format!("git › {}", e.path),
                    _ => format!("git ({})", g.entries.len()),
                }
            }
            (_, Some(r)) => self.vault.notes[r.note].path.display().to_string(),
            _ => format!("notes ({}/{})", self.visible.len(), self.vault.notes.len()),
        };
        let dirty = match self.dirty {
            Some(n) if n > 0 => format!("  ±{n}").yellow(),
            _ => "".into(),
        };
        f.render_widget(Line::from(vec![" kb ".black().on_cyan().bold(), format!(" {} › {crumb}", self.vault.name()).into(), dirty]), head);

        match (&mut self.git, &mut self.reader) {
            (Some(g), _) => match &g.diff {
                Some((lines, scroll)) => f.render_widget(Paragraph::new(lines.clone()).scroll((*scroll, 0)), body),
                None => {
                    let items = g.entries.iter().map(|e| {
                        Line::from(vec![
                            Span::styled(e.x.to_string(), Style::new().green()),
                            Span::styled(e.y.to_string(), Style::new().red()),
                            Span::raw(format!(" {}", e.path)),
                        ])
                    });
                    f.render_stateful_widget(List::new(items).highlight_style(Style::new().reversed()), body, &mut g.list);
                    if g.entries.is_empty() {
                        f.render_widget("nothing to commit, working tree clean".dark_gray(), body);
                    }
                }
            },
            (_, Some(r)) => r.draw(f, body),
            _ => {
                let items = self.visible.iter().map(|&i| {
                    let n = &self.vault.notes[i];
                    let dir = n.path.parent().map(|p| p.display().to_string()).unwrap_or_default();
                    Line::from(vec![Span::raw(n.title.as_str()), format!("  {dir}").dark_gray()])
                });
                f.render_stateful_widget(List::new(items).highlight_style(Style::new().reversed()), body, &mut self.list);
            }
        }

        let view = (self.git.as_ref().map(|g| g.diff.is_some()), self.reader.is_some());
        let footer = match (&self.prompt, self.status.is_empty(), view) {
            (Some(Prompt::Filter), _, _) => Line::from(format!("/{}█", self.filter)),
            (Some(Prompt::Command(c)), _, _) => Line::from(format!(":{c}█")),
            (Some(Prompt::Commit(m)), _, _) => Line::from(vec!["commit message: ".cyan(), format!("{m}█").into()]),
            (_, false, _) => Line::from(self.status.as_str().yellow()),
            (_, _, (Some(true), _)) => "↑↓ j/k scroll  esc back".dark_gray().into(),
            (_, _, (Some(false), _)) => "↑↓ j/k move  ⏎ diff  space stage/unstage  a stage all  c commit  p push  e edit  esc back".dark_gray().into(),
            (_, _, (_, true)) => "↑↓ j/k scroll  tab/S-tab link  ⏎ follow  e edit  [ ] history  esc back".dark_gray().into(),
            _ => "↑↓ j/k move  / filter  ⏎ open  e edit  [ ] history  :git  q quit".dark_gray().into(),
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
        if app.poll() {
            continue;
        }
        if event::poll(Duration::from_millis(100))?
            && let Event::Key(k) = event::read()?
            && k.kind == KeyEventKind::Press
            && !app.key(k.code)
        {
            return Ok(());
        }
        let paths: Vec<_> = rx.try_iter().flatten().flatten().flat_map(|e| e.event.paths).collect();
        if paths.iter().any(|p| relevant(p)) {
            app.reload();
        }
        if !paths.is_empty() {
            app.refresh_git();
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

    fn type_str(a: &mut App, s: &str) {
        s.chars().for_each(|c| {
            a.key(KeyCode::Char(c));
        });
    }

    #[test]
    fn git_view_stage_diff_commit() {
        let (d, mut a) = app();
        git::init(d.path());
        a.refresh_git();
        assert!(screen(&mut a).contains("±2"));
        type_str(&mut a, ":git");
        keys(&mut a, &[KeyCode::Enter]);
        let s = screen(&mut a);
        assert!(s.contains("git (2)") && s.contains("?? A.md"), "{s}");
        keys(&mut a, &[KeyCode::Enter]);
        assert!(screen(&mut a).contains("+Go to [[B]]."));
        keys(&mut a, &[KeyCode::Esc, KeyCode::Char(' ')]);
        assert!(screen(&mut a).contains("A  A.md"));
        keys(&mut a, &[KeyCode::Char('a'), KeyCode::Char('c')]);
        type_str(&mut a, "notes");
        keys(&mut a, &[KeyCode::Enter]);
        let s = screen(&mut a);
        assert!(s.contains("committed") && s.contains("working tree clean") && !s.contains('±'), "{s}");
        keys(&mut a, &[KeyCode::Esc]);
        assert!(screen(&mut a).contains("notes (2/2)"));
    }

    #[test]
    fn git_outside_repo_and_unknown_command() {
        let (_d, mut a) = app();
        type_str(&mut a, ":git");
        keys(&mut a, &[KeyCode::Enter]);
        assert!(screen(&mut a).contains("not a git repository"));
        type_str(&mut a, ":nope");
        keys(&mut a, &[KeyCode::Enter]);
        assert!(screen(&mut a).contains("unknown command: nope"));
        type_str(&mut a, ":q");
        assert!(!a.key(KeyCode::Enter));
    }

    #[test]
    fn quit_from_list() {
        let (_d, mut a) = app();
        assert!(!a.key(KeyCode::Char('q')));
    }
}
