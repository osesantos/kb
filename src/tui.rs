use crate::{
    md::{self, Doc, Link, Seg},
    vault::Vault,
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
            Link::Note(i) => Ok(*i),
            Link::Name(n) => self.vault.resolve(n).ok_or_else(|| n.clone()),
        };
        match target {
            Ok(i) => self.open(i),
            Err(n) if n.contains("://") => self.status = format!("external link: {n}"),
            Err(n) => self.status = format!("unresolved: [[{n}]]"),
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
            (_, _, true) => "j/k scroll  tab/S-tab link  ⏎ follow  [ ] history  esc back".dark_gray().into(),
            (_, _, false) => "j/k move  / filter  ⏎ open  [ ] history  q quit".dark_gray().into(),
        };
        f.render_widget(footer, foot);
    }
}

pub fn run(vault: Vault) -> std::io::Result<()> {
    let mut app = App::new(vault);
    ratatui::run(|term| loop {
        term.draw(|f| app.draw(f))?;
        if let Event::Key(k) = event::read()?
            && k.kind == KeyEventKind::Press
            && !app.key(k.code)
        {
            return Ok(());
        }
    })
}
