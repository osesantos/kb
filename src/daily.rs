//! Daily-note convention: a folder plus a moment.js filename format, as Obsidian's daily-notes plugin stores it.

use crate::config::DailyCfg;
use chrono::{Datelike, NaiveDate};
use std::path::{Path, PathBuf};

#[derive(Debug, PartialEq)]
pub struct Daily {
    pub folder: PathBuf,
    pub format: String,
}

impl Daily {
    /// Per field: config override, else `.obsidian/daily-notes.json`, else Obsidian's defaults.
    pub fn resolve(root: &Path, over: &DailyCfg) -> Daily {
        let json: serde_json::Value = std::fs::read_to_string(root.join(".obsidian/daily-notes.json"))
            .ok()
            .and_then(|s| serde_json::from_str(&s).ok())
            .unwrap_or_default();
        let field = |o: &Option<String>, k: &str| o.clone().or_else(|| json[k].as_str().filter(|s| !s.is_empty()).map(String::from));
        Daily {
            folder: field(&over.folder, "folder").unwrap_or_default().trim_matches('/').into(),
            format: field(&over.format, "format").unwrap_or_else(|| "YYYY-MM-DD".into()),
        }
    }

    /// Vault-relative path of the daily note for `d`.
    pub fn path(&self, d: NaiveDate) -> PathBuf {
        self.folder.join(format!("{}.md", moment(&self.format, d)))
    }
}

const TOKENS: [&str; 13] = ["YYYY", "YY", "MMMM", "MMM", "MM", "M", "Do", "DD", "D", "dddd", "ddd", "WW", "W"];

fn token(t: &str, d: NaiveDate) -> String {
    let n = d.day();
    match t {
        "Do" => format!("{n}{}", if (11..=13).contains(&(n % 100)) { "th" } else { ["th", "st", "nd", "rd"].get(n as usize % 10).unwrap_or(&"th") }),
        "WW" => format!("{:02}", d.iso_week().week()),
        "W" => d.iso_week().week().to_string(),
        t => {
            let f = match t {
                "YYYY" => "%Y",
                "YY" => "%y",
                "MMMM" => "%B",
                "MMM" => "%b",
                "MM" => "%m",
                "M" => "%-m",
                "DD" => "%d",
                "D" => "%-d",
                "dddd" => "%A",
                _ => "%a",
            };
            d.format(f).to_string()
        }
    }
}

/// Formats `d` with the moment.js tokens Obsidian users put in filenames; `[text]` is literal.
pub fn moment(fmt: &str, d: NaiveDate) -> String {
    let mut out = String::new();
    let mut rest = fmt;
    while let Some(c) = rest.chars().next() {
        if let Some((lit, tail)) = rest.strip_prefix('[').and_then(|r| r.split_once(']')) {
            out.push_str(lit);
            rest = tail;
        } else if let Some(t) = TOKENS.iter().find(|t| rest.starts_with(*t)) {
            out.push_str(&token(t, d));
            rest = &rest[t.len()..];
        } else {
            out.push(c);
            rest = &rest[c.len_utf8()..];
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    fn day(y: i32, m: u32, d: u32) -> NaiveDate {
        NaiveDate::from_ymd_opt(y, m, d).unwrap()
    }

    #[test]
    fn formats_moment_tokens() {
        assert_eq!(moment("MMM Do, YYYY", day(2026, 10, 2)), "Oct 2nd, 2026");
        assert_eq!(moment("MMM Do, YYYY", day(2026, 9, 11)), "Sep 11th, 2026");
        assert_eq!(moment("MMM Do, YYYY", day(2026, 9, 21)), "Sep 21st, 2026");
        assert_eq!(moment("MMM Do, YYYY", day(2026, 9, 23)), "Sep 23rd, 2026");
        assert_eq!(moment("YYYY-MM-DD", day(2026, 1, 5)), "2026-01-05");
        assert_eq!(moment("YYYY/MMMM/D-M ddd", day(2026, 1, 5)), "2026/January/5-1 Mon");
        assert_eq!(moment("YYYY-[W]WW dddd", day(2026, 1, 5)), "2026-W02 Monday");
    }

    #[test]
    fn resolves_config_over_obsidian_over_defaults() {
        let d = tempfile::tempdir().unwrap();
        let none = DailyCfg::default();
        assert_eq!(Daily::resolve(d.path(), &none), Daily { folder: "".into(), format: "YYYY-MM-DD".into() });
        std::fs::create_dir(d.path().join(".obsidian")).unwrap();
        std::fs::write(d.path().join(".obsidian/daily-notes.json"), r#"{"format":"MMM Do, YYYY","folder":"Journals/"}"#).unwrap();
        assert_eq!(Daily::resolve(d.path(), &none).path(day(2026, 10, 2)), Path::new("Journals/Oct 2nd, 2026.md"));
        let over = DailyCfg { folder: None, format: Some("YYYY-MM-DD".into()) };
        assert_eq!(Daily::resolve(d.path(), &over).path(day(2026, 10, 2)), Path::new("Journals/2026-10-02.md"));
    }
}
