//! Markdown → styled lines with navigable link anchors, and width-aware wrapping.

use crate::vault::MD_OPTS;
use pulldown_cmark::{Event, HeadingLevel, Parser, Tag, TagEnd};
use ratatui::{
    style::{Color, Modifier, Style},
    text::Span,
};

#[derive(Clone)]
pub struct Seg {
    pub text: String,
    pub style: Style,
    pub link: Option<usize>,
}

#[derive(Clone)]
pub enum Link {
    Name(String),
    Note(usize),
}

#[derive(Default)]
pub struct Doc {
    pub lines: Vec<Vec<Seg>>,
    pub links: Vec<Link>,
}

#[derive(Default)]
struct Builder {
    doc: Doc,
    cur: Vec<Seg>,
    styles: Vec<Style>,
    link: Option<usize>,
    lists: Vec<Option<u64>>,
    quote: usize,
    meta: bool,
    code: bool,
}

fn dim() -> Style {
    Style::new().dark_gray()
}

fn code() -> Style {
    Style::new().fg(Color::Yellow)
}

fn heading(level: HeadingLevel) -> Style {
    match level {
        HeadingLevel::H1 => Style::new().cyan().bold().underlined(),
        HeadingLevel::H2 => Style::new().cyan().bold(),
        _ => Style::new().bold(),
    }
}

impl Builder {
    fn style(&self) -> Style {
        self.styles.iter().fold(Style::default(), |a, s| a.patch(*s))
    }

    fn push(&mut self, text: impl Into<String>, style: Style) {
        if self.cur.is_empty() && self.quote > 0 {
            self.cur.push(Seg { text: "│ ".repeat(self.quote), style: dim(), link: None });
        }
        self.cur.push(Seg { text: text.into(), style, link: self.link });
    }

    fn text(&mut self, t: &str) {
        let s = self.style();
        self.push(t, s)
    }

    fn flush(&mut self) {
        if !self.cur.is_empty() {
            self.doc.lines.push(std::mem::take(&mut self.cur))
        }
    }

    fn blank(&mut self) {
        self.flush();
        if self.doc.lines.last().is_some_and(|l| !l.is_empty()) {
            self.doc.lines.push(vec![])
        }
    }

    fn open_link(&mut self, l: Link) {
        self.link = Some(self.doc.links.len());
        self.doc.links.push(l);
        self.styles.push(Style::new().fg(Color::Blue).underlined())
    }

    fn close_link(&mut self) {
        self.link = None;
        self.styles.pop();
    }
}

/// Renders a note; `backlinks` are appended as a navigable section so they share link focus.
pub fn render<'a>(src: &str, backlinks: impl Iterator<Item = (usize, &'a str)>) -> Doc {
    let mut b = Builder::default();
    for ev in Parser::new_ext(src, MD_OPTS) {
        match ev {
            Event::Start(Tag::MetadataBlock(_)) => b.meta = true,
            Event::End(TagEnd::MetadataBlock(_)) => b.meta = false,
            _ if b.meta => {}
            Event::Start(Tag::Heading { level, .. }) => {
                b.blank();
                let s = heading(level);
                b.styles.push(s);
                b.push(format!("{} ", "#".repeat(level as usize)), s.add_modifier(Modifier::DIM));
            }
            Event::End(TagEnd::Heading(_)) => {
                b.styles.pop();
                b.blank()
            }
            Event::End(TagEnd::Paragraph) => b.blank(),
            Event::Start(Tag::Emphasis) => b.styles.push(Style::new().italic()),
            Event::Start(Tag::Strong) => b.styles.push(Style::new().bold()),
            Event::Start(Tag::Strikethrough) => b.styles.push(Style::new().crossed_out()),
            Event::End(TagEnd::Emphasis | TagEnd::Strong | TagEnd::Strikethrough) => {
                b.styles.pop();
            }
            Event::Start(Tag::BlockQuote(_)) => {
                b.flush();
                b.quote += 1
            }
            Event::End(TagEnd::BlockQuote(_)) => {
                b.flush();
                b.quote -= 1;
                b.blank()
            }
            Event::Start(Tag::CodeBlock(_)) => {
                b.flush();
                b.code = true
            }
            Event::End(TagEnd::CodeBlock) => {
                b.code = false;
                b.blank()
            }
            Event::Text(t) if b.code => t.lines().for_each(|l| {
                b.push(format!("  {l}"), code());
                b.flush()
            }),
            Event::Start(Tag::List(start)) => {
                b.flush();
                b.lists.push(start)
            }
            Event::End(TagEnd::List(_)) => {
                b.lists.pop();
                if b.lists.is_empty() {
                    b.blank()
                }
            }
            Event::Start(Tag::Item) => {
                b.flush();
                let indent = "  ".repeat(b.lists.len().saturating_sub(1));
                let bullet = match b.lists.last_mut() {
                    Some(Some(n)) => {
                        *n += 1;
                        format!("{}. ", *n - 1)
                    }
                    _ => "• ".into(),
                };
                b.push(indent + &bullet, dim())
            }
            Event::End(TagEnd::Item) => b.flush(),
            Event::TaskListMarker(done) => b.push(if done { "[x] " } else { "[ ] " }, dim()),
            Event::Start(Tag::Link { dest_url, .. }) => b.open_link(Link::Name(dest_url.into_string())),
            Event::End(TagEnd::Link) => b.close_link(),
            Event::Start(Tag::Image { dest_url, .. }) => {
                b.push("🖼 ", dim());
                b.open_link(Link::Name(dest_url.into_string()))
            }
            Event::End(TagEnd::Image) => b.close_link(),
            Event::Text(t) => b.text(&t),
            Event::Code(t) => b.push(t.into_string(), code()),
            Event::SoftBreak => b.text(" "),
            Event::HardBreak => b.flush(),
            Event::Rule => {
                b.blank();
                b.push("─".repeat(40), dim());
                b.blank()
            }
            Event::End(TagEnd::TableCell) => b.push(" │ ", dim()),
            Event::End(TagEnd::TableHead | TagEnd::TableRow) => b.flush(),
            Event::End(TagEnd::Table) => b.blank(),
            _ => {}
        }
    }
    let mut backlinks = backlinks.peekable();
    if backlinks.peek().is_some() {
        b.blank();
        b.push("─".repeat(40), dim());
        b.flush();
        b.push("Backlinks", dim().bold());
        b.flush();
        backlinks.for_each(|(i, title)| {
            b.push("← ", dim());
            b.open_link(Link::Note(i));
            b.text(title);
            b.close_link();
            b.flush()
        });
    }
    b.flush();
    while b.doc.lines.last().is_some_and(Vec::is_empty) {
        b.doc.lines.pop();
    }
    b.doc
}

/// Word-wraps logical lines to `width` columns, keeping each word's style and link anchor.
pub fn wrap(lines: &[Vec<Seg>], width: usize) -> Vec<Vec<Seg>> {
    lines
        .iter()
        .flat_map(|line| {
            line.iter()
                .flat_map(|s| s.text.split_inclusive(' ').map(move |w| Seg { text: w.into(), ..s.clone() }))
                .fold((vec![vec![]], 0), |(mut out, col): (Vec<Vec<Seg>>, usize), seg| {
                    let w = Span::raw(seg.text.as_str()).width();
                    let col = if col > 0 && col + w > width {
                        out.push(vec![]);
                        0
                    } else {
                        col
                    };
                    out.last_mut().unwrap().push(seg);
                    (out, col + w)
                })
                .0
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn text(lines: &[Vec<Seg>]) -> Vec<String> {
        lines.iter().map(|l| l.iter().map(|s| s.text.as_str()).collect()).collect()
    }

    fn names(d: &Doc) -> Vec<String> {
        d.links.iter().map(|l| match l { Link::Name(n) => n.clone(), Link::Note(i) => format!("#{i}") }).collect()
    }

    #[test]
    fn collects_wikilinks_urls_and_images_as_links() {
        let d = render("See [[A]], [site](https://x.dev) and ![[pic.png]] ![alt](img/b.jpg)", std::iter::empty());
        assert_eq!(names(&d), ["A", "https://x.dev", "pic.png", "img/b.jpg"]);
    }

    #[test]
    fn link_segments_carry_their_anchor() {
        let d = render("x [[A]] y", std::iter::empty());
        let anchored: Vec<_> = d.lines[0].iter().filter(|s| s.link == Some(0)).map(|s| s.text.as_str()).collect();
        assert_eq!(anchored, ["A"]);
    }

    #[test]
    fn hides_frontmatter_and_renders_blocks() {
        let d = render("---\ntitle: t\n---\n# H\n\n- a\n- [x] b\n\n```\ncode\n```", std::iter::empty());
        assert_eq!(text(&d.lines), ["# H", "", "• a", "• [x] b", "", "  code"]);
    }

    #[test]
    fn appends_backlinks_as_note_links() {
        let d = render("body", [(7, "Other")].into_iter());
        assert_eq!(names(&d), ["#7"]);
        assert_eq!(text(&d.lines).last().unwrap(), "← Other");
    }

    #[test]
    fn wrap_breaks_on_words_and_keeps_anchors() {
        let d = render("aaa bbb [[ccc]]", std::iter::empty());
        let w = wrap(&d.lines, 8);
        assert_eq!(text(&w), ["aaa bbb ", "ccc"]);
        assert_eq!(w[1][0].link, Some(0));
    }
}
