use crate::{
    config::Config,
    daily::Daily,
    git, search,
    md::{self, Doc, Link, Seg},
    vault::{Target, Vault},
};
use chrono::{Datelike, Days, Months, NaiveDate};
use ratatui::{
    Frame,
    crossterm::event::{self, Event, KeyCode, KeyEventKind},
    layout::{Constraint, Layout, Margin, Rect},
    style::{Style, Stylize},
    text::{Line, Span},
    widgets::{List, ListItem, ListState, Paragraph},
};

struct Reader {
    note: usize,
    doc: Doc,
    wrapped: Vec<Vec<Seg>>,
    width: usize,
    height: usize,
    scroll: usize,
    focus: Option<usize>,
    find: Find,
}

/// In-note search: rows of the wrapped text containing the query, and which one is current.
#[derive(Default, Clone)]
struct Find {
    query: String,
    hits: Vec<usize>,
    cur: usize,
    jump: bool,
}

impl Reader {
    fn new(v: &Vault, note: usize) -> Self {
        let backlinks = v.backlinks[note].iter().map(|&i| (i, v.notes[i].title.as_str()));
        let doc = md::render(&v.read(note), backlinks);
        Reader { note, doc, wrapped: vec![], width: 0, height: 0, scroll: 0, focus: None, find: Find::default() }
    }

    fn layout(&mut self, width: usize, height: usize) {
        if width != self.width {
            self.wrapped = md::wrap(&self.doc.lines, width);
            self.width = width;
            self.rehit();
        }
        self.height = height;
        if std::mem::take(&mut self.find.jump)
            && let Some(&y) = self.find.hits.get(self.find.cur)
        {
            self.scroll = y.saturating_sub(height / 3);
            self.scroll_by(0);
        }
    }

    fn rehit(&mut self) {
        let q = self.find.query.to_lowercase();
        self.find.hits = match q.trim() {
            "" => vec![],
            _ => (0..self.wrapped.len())
                .filter(|&y| self.wrapped[y].iter().map(|s| s.text.as_str()).collect::<String>().to_lowercase().contains(&q))
                .collect(),
        };
        self.find.cur = self.find.cur.min(self.find.hits.len().saturating_sub(1));
    }

    fn find(&mut self, q: &str) {
        self.find = Find { query: q.into(), jump: true, ..Find::default() };
        self.rehit();
    }

    fn step(&mut self, d: isize) {
        let n = self.find.hits.len() as isize;
        if n > 0 {
            self.find.cur = (self.find.cur as isize + d).rem_euclid(n) as usize;
            self.find.jump = true;
        }
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
            KeyCode::Esc if !self.find.query.is_empty() => self.find = Find::default(),
            KeyCode::Esc | KeyCode::Char('q') => return true,
            KeyCode::Char('n') => self.step(1),
            KeyCode::Char('N') => self.step(-1),
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
        let words: Vec<String> = self.find.query.split_whitespace().map(str::to_lowercase).collect();
        let current = self.find.hits.get(self.find.cur).copied();
        let lines: Vec<Line> = self.wrapped[self.scroll.min(self.wrapped.len())..]
            .iter()
            .take(self.height)
            .enumerate()
            .map(|(dy, l)| {
                let y = self.scroll + dy;
                let hit = self.find.hits.binary_search(&y).is_ok();
                l.iter()
                    .map(|s| {
                        let focused = s.link.is_some() && s.link == self.focus;
                        let lower = s.text.to_lowercase();
                        let style = match () {
                            _ if hit && words.iter().any(|w| lower.contains(w.as_str())) => {
                                if current == Some(y) { s.style.black().on_yellow() } else { s.style.on_dark_gray() }
                            }
                            _ if focused => s.style.reversed(),
                            _ => s.style,
                        };
                        Span::styled(s.text.as_str(), style)
                    })
                    .collect::<Line>()
            })
            .collect();
        f.render_widget(Paragraph::new(lines), area);
    }
}

enum Prompt {
    Filter,
    Find(String),
    Search(String),
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

/// The search results view; it sits under the reader so Esc from a hit returns here.
struct Results {
    query: String,
    hits: Vec<search::Hit>,
    list: ListState,
}

/// The `:cal` view: a month grid marking which days have a daily note under the vault's convention.
struct Cal {
    daily: Daily,
    cursor: NaiveDate,
    today: NaiveDate,
}

impl Cal {
    fn lines(&self, v: &Vault) -> Vec<Line<'static>> {
        let first = self.cursor - Days::new(u64::from(self.cursor.day0()));
        let last = first.checked_add_months(Months::new(1)).and_then(|d| d.pred_opt()).unwrap_or(first);
        let lead = std::iter::repeat_n([Span::raw(" "), Span::raw("  ")], first.weekday().num_days_from_monday() as usize).flatten();
        let days = first.iter_days().take_while(|&d| d <= last).flat_map(|d| {
            let style = if v.find(&self.daily.path(d)).is_some() { Style::new().cyan().bold() } else { Style::new().dark_gray() };
            let style = if d == self.today { style.underlined() } else { style };
            let style = if d == self.cursor { style.reversed() } else { style };
            [Span::raw(" "), Span::styled(format!("{:>2}", d.day()), style)]
        });
        let cells: Vec<Span> = lead.chain(days).collect();
        let path = self.daily.path(self.cursor);
        let state = if v.find(&path).is_some() { "".into() } else { "  (missing — e creates)".dark_gray() };
        [Line::from(first.format(" %B %Y").to_string().bold()), Line::from(" Mo Tu We Th Fr Sa Su".dark_gray())]
            .into_iter()
            .chain(cells.chunks(14).map(|w| Line::from(w.to_vec())))
            .chain([Line::default(), Line::from(vec![format!(" {}", path.display()).into(), state])])
            .collect()
    }
}

pub struct App {
    cfg: Config,
    vaults: Option<ListState>,
    cal: Option<Cal>,
    results: Option<Results>,
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
    pub fn new(vault: Vault, cfg: Config) -> Self {
        let mut app = App {
            cfg,
            vaults: None,
            cal: None,
            vault,
            filter: String::new(),
            prompt: None,
            results: None,
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

    fn open_cal(&mut self) {
        let today = chrono::Local::now().date_naive();
        let daily = Daily::resolve(&self.vault.root, &self.cfg.daily(&self.vault.root));
        self.close_views();
        self.cal = Some(Cal { daily, cursor: today, today });
    }

    fn close_views(&mut self) {
        self.git = None;
        self.vaults = None;
        self.reader = None;
        self.results = None;
        self.cal = None;
    }

    fn cal_key(&mut self, code: KeyCode) {
        let Some(c) = &mut self.cal else { return };
        let d = c.cursor;
        let moved = match code {
            KeyCode::Char('h') | KeyCode::Left => d.pred_opt(),
            KeyCode::Char('l') | KeyCode::Right => d.succ_opt(),
            KeyCode::Char('k') | KeyCode::Up => d.checked_sub_days(Days::new(7)),
            KeyCode::Char('j') | KeyCode::Down => d.checked_add_days(Days::new(7)),
            KeyCode::Char('H') => d.checked_sub_months(Months::new(1)),
            KeyCode::Char('L') => d.checked_add_months(Months::new(1)),
            KeyCode::Char('t') => Some(c.today),
            _ => None,
        };
        if let Some(m) = moved {
            c.cursor = m;
            return;
        }
        let path = c.daily.path(d);
        match code {
            KeyCode::Esc | KeyCode::Char('q') => self.cal = None,
            KeyCode::Enter => match self.vault.find(&path) {
                Some(i) => self.open(i),
                None => self.status = format!("no daily note {}  (e creates it)", path.display()),
            },
            KeyCode::Char('e') => self.edit = Some(self.vault.root.join(path)),
            _ => {}
        }
    }

    fn open_vaults(&mut self) {
        let cur = self.cfg.vaults.iter().position(|v| v.path == self.vault.root);
        self.close_views();
        self.vaults = Some(ListState::default().with_selected(cur.or((!self.cfg.vaults.is_empty()).then_some(0))));
    }

    fn vaults_key(&mut self, code: KeyCode) {
        let Some(l) = &mut self.vaults else { return };
        match code {
            KeyCode::Esc | KeyCode::Char('q') => self.vaults = None,
            KeyCode::Char('j') | KeyCode::Down => l.select_next(),
            KeyCode::Char('k') | KeyCode::Up => l.select_previous(),
            KeyCode::Enter => {
                if let Some(v) = l.selected().and_then(|i| self.cfg.vaults.get(i)).cloned() {
                    let cfg = std::mem::take(&mut self.cfg);
                    *self = App::new(Vault::load(&v.path), cfg);
                    self.status = format!("switched to {}", v.name);
                }
            }
            _ => {}
        }
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
        self.vaults = None;
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
            Prompt::Command(s) | Prompt::Commit(s) | Prompt::Find(s) | Prompt::Search(s) => s,
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
            Prompt::Find(q) => {
                if let Some(r) = &mut self.reader {
                    r.find(&q)
                }
            }
            Prompt::Search(q) => self.search(q.trim()),
            Prompt::Command(c) => match c.trim().split_once(' ').unwrap_or((c.trim(), "")) {
                ("git" | "g", _) => self.open_git(),
                ("notes" | "n", _) => self.close_views(),
                ("cal" | "c", _) => self.open_cal(),
                ("vaults" | "v", _) => self.open_vaults(),
                ("search" | "s", "") => self.prompt = Some(Prompt::Search(String::new())),
                ("search" | "s", q) => self.search(q.trim()),
                ("q" | "quit", _) => self.quit = true,
                (other, _) => self.status = format!("unknown command: {other}  (notes, search, git, cal, vaults, quit)"),
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
        let reader = self.reader.as_ref().map(|r| (path(r.note), r.scroll, r.focus, r.find.query.clone()));

        self.vault = Vault::load(&self.vault.root);
        let find = |v: &Vault, p: &std::path::Path| v.notes.iter().position(|n| n.path == p);

        self.history = hist.iter().filter_map(|p| find(&self.vault, p)).collect();
        self.pos = self.pos.min(self.history.len().saturating_sub(1));
        self.refilter();
        if let Some(s) = sel.and_then(|p| find(&self.vault, &p)).and_then(|i| self.visible.iter().position(|&v| v == i)) {
            self.list.select(Some(s));
        }
        if let Some(r) = self.results.take() {
            let sel = r.list.selected();
            self.search(&r.query);
            if let Some(nr) = &mut self.results {
                nr.list.select(sel.map(|s| s.min(nr.hits.len().saturating_sub(1))).filter(|_| !nr.hits.is_empty()));
            }
        }
        self.reader = reader.and_then(|(p, scroll, focus, query)| {
            let Some(i) = find(&self.vault, &p) else {
                self.status = format!("{} was removed", p.display());
                return None;
            };
            let mut r = Reader::new(&self.vault, i);
            r.scroll = scroll;
            r.focus = focus.filter(|&f| f < r.doc.links.len());
            r.find.query = query;
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

    fn search(&mut self, q: &str) {
        let hits = search::lexical(&self.vault, q, 500);
        let mut list = ListState::default();
        list.select((!hits.is_empty()).then_some(0));
        self.close_views();
        self.results = Some(Results { query: q.into(), hits, list });
    }

    fn results_key(&mut self, code: KeyCode) {
        let Some(r) = &mut self.results else { return };
        let sel = r.list.selected().and_then(|i| r.hits.get(i)).map(|h| h.note);
        match (code, sel) {
            (KeyCode::Esc | KeyCode::Char('q'), _) => self.results = None,
            (KeyCode::Char('j') | KeyCode::Down, _) => r.list.select_next(),
            (KeyCode::Char('k') | KeyCode::Up, _) => r.list.select_previous(),
            (KeyCode::Char('s'), _) => self.prompt = Some(Prompt::Search(String::new())),
            (KeyCode::Char('e'), Some(i)) => self.edit = Some(self.vault.root.join(&self.vault.notes[i].path)),
            (KeyCode::Enter, Some(i)) => {
                let q = r.query.clone();
                self.open(i);
                if let Some(rd) = &mut self.reader {
                    rd.find(&q)
                }
            }
            _ => {}
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
        if self.vaults.is_some() {
            self.vaults_key(code);
            return true;
        }
        if self.reader.is_none() && self.cal.is_some() && !matches!(code, KeyCode::Char('[' | ']')) {
            self.cal_key(code);
            return true;
        }
        if self.reader.is_none() && self.results.is_some() && !matches!(code, KeyCode::Char('[' | ']')) {
            self.results_key(code);
            return true;
        }
        if self.reader.is_some() && code == KeyCode::Char('/') {
            self.prompt = Some(Prompt::Find(String::new()));
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
                    KeyCode::Char('s') => self.prompt = Some(Prompt::Search(String::new())),
                    _ => {}
                },
            },
        }
        true
    }

    fn draw(&mut self, f: &mut Frame) {
        let [head, body, foot] = Layout::vertical([Constraint::Length(1), Constraint::Fill(1), Constraint::Length(1)]).areas(f.area());
        let crumb = match (&self.git, &self.reader, &self.results) {
            _ if self.vaults.is_some() => format!("vaults ({})", self.cfg.vaults.len()),
            (Some(g), _, _) => {
                let sel = g.list.selected().and_then(|i| g.entries.get(i));
                match (&g.diff, sel) {
                    (Some(_), Some(e)) => format!("git › {}", e.path),
                    _ => format!("git ({})", g.entries.len()),
                }
            }
            (_, Some(r), _) => self.vault.notes[r.note].path.display().to_string(),
            (_, _, Some(s)) => format!("search \"{}\" ({})", s.query, s.hits.len()),
            _ if self.cal.is_some() => "cal".into(),
            _ => format!("notes ({}/{})", self.visible.len(), self.vault.notes.len()),
        };
        let dirty = match self.dirty {
            Some(n) if n > 0 => format!("  ±{n}").yellow(),
            _ => "".into(),
        };
        f.render_widget(Line::from(vec![" kb ".black().on_cyan().bold(), format!(" {} › {crumb}", self.vault.name()).into(), dirty]), head);

        match (&mut self.git, &mut self.reader, &mut self.results) {
            _ if self.vaults.is_some() => {
                let cur = &self.vault.root;
                let items = self.cfg.vaults.iter().map(|v| {
                    let mark = if &v.path == cur { "● " } else { "  " };
                    Line::from(vec![mark.cyan(), v.name.as_str().bold(), format!("  {}", v.path.display()).dark_gray()])
                });
                if let Some(l) = &mut self.vaults {
                    f.render_stateful_widget(List::new(items).highlight_style(Style::new().reversed()), body, l);
                }
                if self.cfg.vaults.is_empty() {
                    f.render_widget(format!("no [[vault]] entries in {}", crate::config::path().display()).dark_gray(), body);
                }
            }
            (Some(g), _, _) => match &g.diff {
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
            (_, Some(r), _) => r.draw(f, body),
            (_, _, Some(s)) => {
                let items = s.hits.iter().map(|h| {
                    let n = &self.vault.notes[h.note];
                    let (ln, snippet) = h.line.as_ref().map_or((String::new(), ""), |(n, l)| (format!(":{n}"), l.as_str()));
                    ListItem::new(vec![
                        Line::from(vec![n.title.as_str().bold(), format!("  {}{ln}", n.path.display()).dark_gray()]),
                        Line::from(format!("  {snippet}")),
                    ])
                });
                f.render_stateful_widget(List::new(items).highlight_style(Style::new().reversed()), body, &mut s.list);
                if s.hits.is_empty() {
                    f.render_widget("no matches".dark_gray(), body);
                }
            }
            _ if self.cal.is_some() => {
                if let Some(c) = &self.cal {
                    f.render_widget(Paragraph::new(c.lines(&self.vault)), body);
                }
            }
            _ => {
                let items = self.visible.iter().map(|&i| {
                    let n = &self.vault.notes[i];
                    let dir = n.path.parent().map(|p| p.display().to_string()).unwrap_or_default();
                    Line::from(vec![Span::raw(n.title.as_str()), format!("  {dir}").dark_gray()])
                });
                f.render_stateful_widget(List::new(items).highlight_style(Style::new().reversed()), body, &mut self.list);
            }
        }

        let view = (self.git.as_ref().map(|g| g.diff.is_some()), self.reader.as_ref().map(|r| (r.find.cur, r.find.hits.len(), r.find.query.is_empty())));
        let footer = match (&self.prompt, self.status.is_empty(), view) {
            (Some(Prompt::Filter), _, _) => Line::from(format!("/{}█", self.filter)),
            (Some(Prompt::Find(q)), _, _) => Line::from(format!("find in note: {q}█")),
            (Some(Prompt::Search(q)), _, _) => Line::from(vec!["search: ".cyan(), format!("{q}█").into()]),
            (Some(Prompt::Command(c)), _, _) => Line::from(format!(":{c}█")),
            (Some(Prompt::Commit(m)), _, _) => Line::from(vec!["commit message: ".cyan(), format!("{m}█").into()]),
            (_, false, _) => Line::from(self.status.as_str().yellow()),
            _ if self.vaults.is_some() => "↑↓ j/k move  ⏎ switch  esc back".dark_gray().into(),
            (_, _, (Some(true), _)) => "↑↓ j/k scroll  esc back".dark_gray().into(),
            (_, _, (Some(false), _)) => "↑↓ j/k move  ⏎ diff  space stage/unstage  a stage all  c commit  p push  e edit  esc back".dark_gray().into(),
            (_, _, (_, Some((cur, n, false)))) => format!("match {}/{n}  n/N next/prev  esc clear", if n == 0 { 0 } else { cur + 1 }).dark_gray().into(),
            (_, _, (_, Some(_))) => "↑↓ j/k scroll  / find  tab/S-tab link  ⏎ follow  e edit  [ ] history  esc back".dark_gray().into(),
            _ if self.cal.is_some() => "h/l day  j/k week  H/L month  t today  ⏎ open  e edit/create  esc back".dark_gray().into(),
            _ if self.results.is_some() => "↑↓ j/k move  ⏎ open  s new search  e edit  esc back".dark_gray().into(),
            _ => "↑↓ j/k move  / filter  s search  ⏎ open  e edit  [ ] history  :git :cal :vaults  q quit".dark_gray().into(),
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

/// Suspends the TUI, runs `$VISUAL`/`$EDITOR` (may contain args) on `path`, then restores it; creates missing parent dirs so new notes can be saved.
fn edit(term: &mut ratatui::DefaultTerminal, path: &std::path::Path) -> Option<String> {
    let editor = std::env::var("VISUAL").or_else(|_| std::env::var("EDITOR")).unwrap_or_else(|_| "vi".into());
    if let Some(dir) = path.parent() {
        let _ = std::fs::create_dir_all(dir);
    }
    ratatui::restore();
    let status = std::process::Command::new("sh").arg("-c").arg(format!("{editor} \"$1\"")).arg("sh").arg(path).status();
    *term = ratatui::init();
    match status {
        Ok(s) if s.success() => None,
        Ok(s) => Some(format!("{editor} exited with {s}")),
        Err(e) => Some(format!("{editor}: {e}")),
    }
}

pub fn run(vault: Vault, cfg: Config) -> std::io::Result<()> {
    use notify_debouncer_full::{DebounceEventResult, new_debouncer, notify::RecursiveMode};
    use std::time::Duration;

    let (tx, rx) = std::sync::mpsc::channel::<DebounceEventResult>();
    let mut watcher = new_debouncer(Duration::from_millis(200), None, tx).map_err(std::io::Error::other)?;
    watcher.watch(&vault.root, RecursiveMode::Recursive).map_err(std::io::Error::other)?;

    let mut watched = vault.root.clone();
    let mut app = App::new(vault, cfg);
    ratatui::run(|term| loop {
        if app.vault.root != watched {
            let _ = watcher.unwatch(&watched);
            watcher.watch(&app.vault.root, RecursiveMode::Recursive).map_err(std::io::Error::other)?;
            watched = app.vault.root.clone();
        }
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
        let a = App::new(Vault::load(d.path()), Config::default());
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
    fn search_opens_note_at_first_match_and_back_returns_to_results() {
        let (_d, mut a) = app();
        keys(&mut a, &[KeyCode::Char('s')]);
        type_str(&mut a, "nowhere");
        keys(&mut a, &[KeyCode::Enter]);
        let s = screen(&mut a);
        assert!(s.contains("search \"nowhere\" (1)") && s.contains("B.md:3"), "{s}");
        keys(&mut a, &[KeyCode::Enter]);
        let s = screen(&mut a);
        assert!(s.contains("› B.md") && s.contains("match 1/1"), "{s}");
        keys(&mut a, &[KeyCode::Esc, KeyCode::Esc]);
        assert!(screen(&mut a).contains("search \"nowhere\""));
        type_str(&mut a, ":s zzz");
        keys(&mut a, &[KeyCode::Enter]);
        assert!(screen(&mut a).contains("no matches"));
    }

    #[test]
    fn find_in_note_cycles_matches() {
        let (d, mut a) = app();
        std::fs::write(d.path().join("A.md"), "one x\n\ntwo x\n\nthree x").unwrap();
        a.reload();
        keys(&mut a, &[KeyCode::Enter, KeyCode::Char('/')]);
        type_str(&mut a, "X");
        keys(&mut a, &[KeyCode::Enter]);
        assert!(screen(&mut a).contains("match 1/3"));
        keys(&mut a, &[KeyCode::Char('N')]);
        assert!(screen(&mut a).contains("match 3/3"));
        keys(&mut a, &[KeyCode::Esc]);
        assert!(screen(&mut a).contains("/ find"), "esc clears search, stays in note");
    }

    #[test]
    fn cal_opens_today_from_obsidian_convention_and_offers_create() {
        let (d, mut a) = app();
        let today = chrono::Local::now().date_naive();
        std::fs::create_dir_all(d.path().join(".obsidian")).unwrap();
        std::fs::write(d.path().join(".obsidian/daily-notes.json"), r#"{"folder":"J","format":"YYYY-MM-DD"}"#).unwrap();
        std::fs::create_dir(d.path().join("J")).unwrap();
        std::fs::write(d.path().join(format!("J/{today}.md")), "today").unwrap();
        a.reload();
        type_str(&mut a, ":cal");
        keys(&mut a, &[KeyCode::Enter]);
        let s = screen(&mut a);
        assert!(s.contains(&today.format("%B %Y").to_string()) && s.contains(&format!("J/{today}.md")), "{s}");
        keys(&mut a, &[KeyCode::Enter]);
        assert!(screen(&mut a).contains(&format!("› J/{today}.md")));
        keys(&mut a, &[KeyCode::Esc, KeyCode::Char('l'), KeyCode::Enter]);
        let tomorrow = today.succ_opt().unwrap();
        assert!(screen(&mut a).contains("no daily note"));
        keys(&mut a, &[KeyCode::Char('e')]);
        assert_eq!(a.edit, Some(d.path().join(format!("J/{tomorrow}.md"))));
    }

    #[test]
    fn vaults_lists_config_and_switches() {
        let (d, a) = app();
        let other = tempfile::tempdir().unwrap();
        std::fs::write(other.path().join("Z.md"), "z").unwrap();
        let vault = |name: &str, p: &std::path::Path| crate::config::VaultCfg { name: name.into(), path: p.to_path_buf(), daily: Default::default() };
        let cfg = Config { vaults: vec![vault("one", d.path()), vault("two", other.path())] };
        let mut a = App::new(a.vault, cfg);
        type_str(&mut a, ":vaults");
        keys(&mut a, &[KeyCode::Enter]);
        let s = screen(&mut a);
        assert!(s.contains("vaults (2)") && s.contains("● one") && s.contains("two"), "{s}");
        keys(&mut a, &[KeyCode::Down, KeyCode::Enter]);
        let s = screen(&mut a);
        assert!(s.contains("switched to two") && s.contains("notes (1/1)"), "{s}");
        assert_eq!(a.vault.root, other.path());
    }

    #[test]
    fn quit_from_list() {
        let (_d, mut a) = app();
        assert!(!a.key(KeyCode::Char('q')));
    }
}
