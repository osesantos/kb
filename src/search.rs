//! Section-level BM25 over the in-memory vault: hits are heading sections, so agents read a section, not a file.

use crate::vault::{Note, Vault};
use rayon::prelude::*;
use std::ops::Range;

/// A `##`/`###` section of a note (the `#` title, frontmatter and intro form the first); long sections are split at blank lines.
#[derive(Debug, Clone, PartialEq)]
pub struct Section {
    pub heading: String,
    /// 1-based inclusive line range in the note.
    pub lines: (usize, usize),
    body: Range<usize>,
    lower: Range<usize>,
}

impl Section {
    pub fn text<'a>(&self, n: &'a Note) -> &'a str {
        &n.body[self.body.clone()]
    }

    /// Rough token count (4 bytes per token), enough to budget agent reads.
    pub fn tokens(&self) -> usize {
        self.body.len().div_ceil(4)
    }
}

const MAX_SECTION: usize = 6 * 1024;
const K1: f32 = 1.2;
const B: f32 = 0.75;
const PER_NOTE: usize = 3;

fn heading(l: &str) -> Option<(usize, &str)> {
    let n = l.bytes().take_while(|&b| b == b'#').count();
    ((1..=6).contains(&n) && l[n..].starts_with(' ')).then(|| (n, l[n..].trim()))
}

/// Each line (newline kept) with its ATX heading level and text; `#` inside code fences is not a heading.
fn outline(body: &str) -> impl Iterator<Item = (&str, Option<(usize, &str)>)> {
    let mut fence = false;
    body.split_inclusive('\n').map(move |l| {
        let t = l.trim_start();
        if t.starts_with("```") || t.starts_with("~~~") {
            fence = !fence;
            return (l, None);
        }
        (l, if fence { None } else { heading(l) })
    })
}

/// Splits a note into sections; `lower` is `body` lowercased, which has the same lines.
pub fn sections(body: &str, lower: &str) -> Vec<Section> {
    let mut out: Vec<Section> = vec![];
    let (mut b, mut lo) = (0, 0);
    for (i, ((l, h), ll)) in outline(body).zip(lower.split_inclusive('\n')).enumerate() {
        let head = h.filter(|&(lvl, _)| (2..=3).contains(&lvl)).map(|(_, t)| t);
        let split = match out.last() {
            None => true,
            Some(s) => head.is_some() || (s.body.len() > MAX_SECTION && l.trim().is_empty()),
        };
        if split {
            let heading = head.map(String::from).or_else(|| out.last().map(|s| s.heading.clone())).unwrap_or_default();
            out.push(Section { heading, lines: (i + 1, i + 1), body: b..b, lower: lo..lo });
        }
        if let Some(s) = out.last_mut() {
            s.lines.1 = i + 1;
            s.body.end = b + l.len();
            s.lower.end = lo + ll.len();
        }
        b += l.len();
        lo += ll.len();
    }
    if out.is_empty() {
        out.push(Section { heading: String::new(), lines: (1, 1), body: 0..0, lower: 0..0 });
    }
    out
}

/// The block under the heading named `want` (case-insensitive), down to the next heading of the same or higher level.
pub fn block<'a>(body: &'a str, want: &str) -> Option<&'a str> {
    let want = want.trim().to_lowercase();
    let mut off = 0;
    let mut start: Option<(usize, usize)> = None;
    for (l, h) in outline(body) {
        match (h, start) {
            (Some((lvl, _)), Some((s, sl))) if lvl <= sl => return Some(&body[s..off]),
            (Some((lvl, t)), None) if t.to_lowercase() == want => start = Some((off, lvl)),
            _ => {}
        }
        off += l.len();
    }
    start.map(|(s, _)| &body[s..])
}

/// Occurrences of `w` starting at a word boundary, so `commit` also matches `commits` but not `recommit`.
fn count(hay: &str, w: &str) -> usize {
    hay.match_indices(w).filter(|(i, _)| hay[..*i].chars().next_back().is_none_or(|c| !c.is_alphanumeric())).count()
}

#[derive(Debug, Clone)]
pub struct Hit {
    pub note: usize,
    pub section: usize,
    pub score: f32,
    /// 1-based line number and text of the first section line matching the query.
    pub line: Option<(usize, String)>,
}

/// BM25-ranked sections of notes that contain every query word (in path, title or body), at most three per note.
pub fn lexical(v: &Vault, query: &str, limit: usize) -> Vec<Hit> {
    let words: Vec<String> = query.split_whitespace().map(str::to_lowercase).collect();
    if words.is_empty() {
        return vec![];
    }
    let tfs: Vec<Vec<Vec<usize>>> = v.notes.par_iter().map(|n| n.sections.iter().map(|s| words.iter().map(|w| count(&n.lower[s.lower.clone()], w)).collect()).collect()).collect();
    let total = tfs.iter().map(Vec::len).sum::<usize>().max(1) as f32;
    let avg = v.notes.iter().flat_map(|n| &n.sections).map(|s| s.lower.len()).sum::<usize>() as f32 / total;
    let idf: Vec<f32> = (0..words.len())
        .map(|k| {
            let df = tfs.iter().flatten().filter(|t| t[k] > 0).count() as f32;
            (1.0 + (total - df + 0.5) / (df + 0.5)).ln()
        })
        .collect();
    let phrase = words.join(" ");
    let mut hits: Vec<Hit> = v
        .notes
        .iter()
        .zip(&tfs)
        .enumerate()
        .filter(|(_, (n, tf))| {
            let path = n.path.to_string_lossy().to_lowercase();
            words.iter().enumerate().all(|(k, w)| path.contains(w.as_str()) || tf.iter().any(|t| t[k] > 0))
        })
        .flat_map(|(i, (n, tf))| {
            let title = n.title.to_lowercase();
            let boost = 1.0 + 0.1 * (1.0 + v.backlinks[i].len() as f32).ln();
            let titles: f32 = words.iter().zip(&idf).filter(|(w, _)| title.contains(w.as_str())).map(|(_, idf)| 2.0 * idf).sum();
            let hit = |si: usize, score: f32| Hit { note: i, section: si, score: (score + titles) * boost, line: first_line(n, &n.sections[si], &phrase, &words[0]) };
            let mut secs: Vec<Hit> = n
                .sections
                .iter()
                .zip(tf)
                .enumerate()
                .filter_map(|(si, (s, t))| {
                    let head = s.heading.to_lowercase();
                    let norm = K1 * (1.0 - B + B * s.lower.len() as f32 / avg);
                    let bm: f32 = t.iter().zip(&idf).map(|(&f, idf)| idf * f as f32 * (K1 + 1.0) / (f as f32 + norm)).sum();
                    let heads: f32 = words.iter().zip(&idf).filter(|(w, _)| head.contains(w.as_str())).map(|(_, idf)| idf).sum();
                    (bm + heads > 0.0).then(|| hit(si, bm + heads))
                })
                .collect();
            if secs.is_empty() {
                secs.push(hit(0, 0.0));
            }
            secs.sort_by(|a, b| b.score.total_cmp(&a.score));
            secs.truncate(PER_NOTE);
            secs
        })
        .collect();
    hits.sort_by(|a, b| b.score.total_cmp(&a.score).then_with(|| v.notes[a.note].path.cmp(&v.notes[b.note].path)).then(a.section.cmp(&b.section)));
    hits.truncate(limit);
    hits
}

fn first_line(n: &Note, s: &Section, phrase: &str, first: &str) -> Option<(usize, String)> {
    let lower = &n.lower[s.lower.clone()];
    let i = lower.lines().position(|l| l.contains(phrase)).or_else(|| lower.lines().position(|l| l.contains(first)))?;
    Some((s.lines.0 + i, s.text(n).lines().nth(i)?.trim().chars().take(160).collect()))
}

/// Hits as JSON; with a token `budget`, each hit carries its section `text` until the budget is spent.
pub fn json(v: &Vault, hits: &[Hit], budget: Option<usize>) -> String {
    let mut left = budget.unwrap_or(0);
    serde_json::Value::Array(
        hits.iter()
            .map(|h| {
                let n = &v.notes[h.note];
                let s = &n.sections[h.section];
                let mut j = serde_json::json!({
                    "path": n.path, "title": n.title, "heading": s.heading, "score": (f64::from(h.score) * 100.0).round() / 100.0, "provider": "lexical",
                    "line": h.line.as_ref().map(|l| l.0), "snippet": h.line.as_ref().map(|l| &l.1),
                    "line_start": s.lines.0, "line_end": s.lines.1, "tokens": s.tokens(),
                });
                if budget.is_some() && s.tokens() <= left {
                    left -= s.tokens();
                    j["text"] = s.text(n).into();
                }
                j
            })
            .collect(),
    )
    .to_string()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn vault() -> (tempfile::TempDir, Vault) {
        let d = tempfile::tempdir().unwrap();
        std::fs::write(d.path().join("Agent Memory.md"), "# Agent memory\n\nstored in qdrant").unwrap();
        std::fs::write(d.path().join("Other.md"), "intro\n## Design\nthe agent memory design\nmemory memory\n## Later\nunrelated agent").unwrap();
        std::fs::write(d.path().join("None.md"), "nothing").unwrap();
        let v = Vault::load(d.path());
        (d, v)
    }

    fn secs(body: &str) -> Vec<(String, (usize, usize))> {
        sections(body, &body.to_lowercase()).into_iter().map(|s| (s.heading, s.lines)).collect()
    }

    #[test]
    fn splits_at_h2_h3_outside_fences() {
        let body = "---\ntags: x\n---\n# Title\nintro\n## A\n```\n## not a heading\n```\n#### deep\n### B\ntext";
        assert_eq!(secs(body), [("".into(), (1, 5)), ("A".into(), (6, 10)), ("B".into(), (11, 12))]);
        assert_eq!(secs(""), [("".into(), (1, 1))]);
    }

    #[test]
    fn splits_long_sections_at_blank_lines() {
        let para = format!("{}\n\n", "x".repeat(4000));
        let s = secs(&format!("## Big\n{para}{para}{para}"));
        assert!(s.len() > 1 && s.iter().all(|(h, _)| h == "Big"), "{s:?}");
    }

    #[test]
    fn block_includes_subheadings_until_same_level() {
        let body = "# T\n## Scope\nin\n### Sub\nmore\n## Next\nout";
        assert_eq!(block(body, "scope"), Some("## Scope\nin\n### Sub\nmore\n"));
        assert_eq!(block(body, "Next"), Some("## Next\nout"));
        assert_eq!(block(body, "nope"), None);
    }

    #[test]
    fn counts_from_word_starts_only() {
        assert_eq!(count("commit commits recommit", "commit"), 2);
    }

    #[test]
    fn requires_all_words_and_ranks_best_section_first() {
        let (_d, v) = vault();
        let hits = lexical(&v, "Agent MEMORY", 10);
        let got: Vec<_> = hits.iter().map(|h| (v.notes[h.note].title.as_str(), v.notes[h.note].sections[h.section].heading.as_str())).collect();
        assert_eq!(got[..2], [("Agent Memory", ""), ("Other", "Design")]);
        assert_eq!(hits[1].line, Some((3, "the agent memory design".into())));
        assert!(lexical(&v, "agent nope", 10).is_empty());
        assert!(lexical(&v, "  ", 10).is_empty());
    }

    #[test]
    fn shorter_section_wins_on_equal_matches() {
        let d = tempfile::tempdir().unwrap();
        std::fs::write(d.path().join("N.md"), format!("## Long\nzeta {}\n## Short\nzeta\n", "filler ".repeat(200))).unwrap();
        let v = Vault::load(d.path());
        let hits = lexical(&v, "zeta", 10);
        assert_eq!(v.notes[0].sections[hits[0].section].heading, "Short");
    }

    #[test]
    fn json_has_contract_fields_and_respects_budget() {
        let (_d, v) = vault();
        let hits = lexical(&v, "agent", 10);
        let j: serde_json::Value = serde_json::from_str(&json(&v, &hits, None)).unwrap();
        assert_eq!(j[0]["provider"], "lexical");
        assert!(j[0]["line_start"].is_u64() && j[0]["tokens"].is_u64() && j[0].get("text").is_none());
        let q: serde_json::Value = serde_json::from_str(&json(&v, &lexical(&v, "qdrant", 10), None)).unwrap();
        assert_eq!((&q[0]["path"], &q[0]["line"]), (&"Agent Memory.md".into(), &3.into()));
        let first = j[0]["tokens"].as_u64().unwrap() as usize;
        let b: serde_json::Value = serde_json::from_str(&json(&v, &hits, Some(first))).unwrap();
        assert!(b[0]["text"].is_string() && b.as_array().unwrap()[1..].iter().all(|h| h.get("text").is_none() || h["tokens"] == 0));
    }
}
