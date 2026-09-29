//! Lexical search over the in-memory vault. Semantic providers plug in beside this later.

use crate::vault::Vault;

#[derive(Debug, Clone)]
pub struct Hit {
    pub note: usize,
    pub score: usize,
    /// 1-based line number and text of the first body line matching the query.
    pub line: Option<(usize, String)>,
}

/// Notes containing every query word in path, title or body; title matches rank first.
pub fn lexical(v: &Vault, query: &str, limit: usize) -> Vec<Hit> {
    let words: Vec<String> = query.split_whitespace().map(str::to_lowercase).collect();
    let Some(first) = words.first() else { return vec![] };
    let phrase = words.join(" ");
    let mut hits: Vec<Hit> = v
        .notes
        .iter()
        .enumerate()
        .filter_map(|(i, n)| {
            let path = n.path.to_string_lossy().to_lowercase();
            if !words.iter().all(|w| path.contains(w.as_str()) || n.lower.contains(w.as_str())) {
                return None;
            }
            let title = n.title.to_lowercase();
            let score = usize::from(title.contains(&phrase)) * 100
                + words.iter().filter(|w| title.contains(w.as_str())).count() * 10
                + words.iter().map(|w| n.lower.matches(w.as_str()).take(10).count()).sum::<usize>();
            let line = n
                .lower
                .lines()
                .position(|l| l.contains(&phrase))
                .or_else(|| n.lower.lines().position(|l| l.contains(first.as_str())))
                .and_then(|i| Some((i + 1, n.body.lines().nth(i)?.trim().chars().take(160).collect())));
            Some(Hit { note: i, score, line })
        })
        .collect();
    hits.sort_by(|a, b| b.score.cmp(&a.score).then_with(|| v.notes[a.note].path.cmp(&v.notes[b.note].path)));
    hits.truncate(limit);
    hits
}

pub fn json(v: &Vault, hits: &[Hit]) -> String {
    serde_json::Value::Array(
        hits.iter()
            .map(|h| {
                let n = &v.notes[h.note];
                serde_json::json!({
                    "path": n.path, "title": n.title, "score": h.score, "provider": "lexical",
                    "line": h.line.as_ref().map(|l| l.0), "snippet": h.line.as_ref().map(|l| &l.1),
                })
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
        std::fs::write(d.path().join("Other.md"), "intro\nthe agent memory design\nmemory memory").unwrap();
        std::fs::write(d.path().join("None.md"), "nothing").unwrap();
        let v = Vault::load(d.path());
        (d, v)
    }

    #[test]
    fn requires_all_words_and_ranks_title_first() {
        let (_d, v) = vault();
        let hits = lexical(&v, "Agent MEMORY", 10);
        let titles: Vec<_> = hits.iter().map(|h| v.notes[h.note].title.as_str()).collect();
        assert_eq!(titles, ["Agent Memory", "Other"]);
        assert_eq!(hits[1].line, Some((2, "the agent memory design".into())));
        assert!(lexical(&v, "agent nope", 10).is_empty());
        assert!(lexical(&v, "  ", 10).is_empty());
    }

    #[test]
    fn json_has_contract_fields() {
        let (_d, v) = vault();
        let j: serde_json::Value = serde_json::from_str(&json(&v, &lexical(&v, "qdrant", 10))).unwrap();
        assert_eq!(j[0]["path"], "Agent Memory.md");
        assert_eq!(j[0]["provider"], "lexical");
        assert_eq!(j[0]["line"], 3);
    }
}
