use pulldown_cmark::{Event, LinkType, Options, Parser, Tag};
use rayon::prelude::*;
use std::{
    collections::HashMap,
    path::{Path, PathBuf},
};

pub const MD_OPTS: Options = Options::ENABLE_WIKILINKS
    .union(Options::ENABLE_YAML_STYLE_METADATA_BLOCKS)
    .union(Options::ENABLE_TABLES)
    .union(Options::ENABLE_STRIKETHROUGH)
    .union(Options::ENABLE_TASKLISTS);

pub struct Note {
    pub path: PathBuf,
    pub title: String,
    pub links: Vec<String>,
    pub body: String,
    /// Lowercased body, kept so search never re-lowercases the vault per query.
    pub lower: String,
    pub sections: Vec<crate::search::Section>,
}

/// A directory of Markdown notes plus the derived link index; rebuilt from disk, never persisted.
pub struct Vault {
    pub root: PathBuf,
    pub notes: Vec<Note>,
    pub backlinks: Vec<Vec<usize>>,
    by_name: HashMap<String, Vec<usize>>,
    files: HashMap<String, PathBuf>,
}

/// What a link target points at once resolved against the vault.
#[derive(Debug, PartialEq)]
pub enum Target {
    Note(usize),
    File(PathBuf),
    Url(String),
    Missing,
}

/// Normalises a link target to the case-insensitive basename Obsidian resolves by.
pub fn key(target: &str) -> String {
    let t = target.split(['#', '^']).next().unwrap_or("");
    let base = t.rsplit('/').next().unwrap_or(t);
    base.strip_suffix(".md").unwrap_or(base).trim().to_lowercase()
}

fn wikilinks(src: &str) -> Vec<String> {
    Parser::new_ext(src, MD_OPTS)
        .filter_map(|e| match e {
            Event::Start(Tag::Link { link_type: LinkType::WikiLink { .. }, dest_url, .. }) => Some(dest_url.into_string()),
            _ => None,
        })
        .collect()
}

impl Vault {
    pub fn load(root: &Path) -> Vault {
        let (md, other): (Vec<PathBuf>, Vec<PathBuf>) = ignore::WalkBuilder::new(root)
            .filter_entry(|e| e.file_name() != ".obsidian")
            .build()
            .filter_map(Result::ok)
            .filter(|e| e.file_type().is_some_and(|t| t.is_file()))
            .map(|e| e.into_path())
            .partition(|p| p.extension().is_some_and(|x| x == "md"));
        let files = other
            .into_iter()
            .filter_map(|p| Some((p.file_name()?.to_string_lossy().to_lowercase(), p)))
            .collect();
        let mut notes: Vec<Note> = md
            .into_par_iter()
            .filter_map(|p| {
                let src = std::fs::read_to_string(&p).ok()?;
                let path = p.strip_prefix(root).ok()?.to_path_buf();
                let lower = src.to_lowercase();
                let sections = crate::search::sections(&src, &lower);
                Some(Note { title: path.file_stem()?.to_string_lossy().into_owned(), links: wikilinks(&src), lower, sections, body: src, path })
            })
            .collect();
        notes.sort_by(|a, b| a.path.cmp(&b.path));

        let by_name = notes.iter().enumerate().fold(HashMap::<_, Vec<_>>::new(), |mut m, (i, n)| {
            m.entry(key(&n.title)).or_default().push(i);
            m
        });
        let mut v = Vault { root: root.to_path_buf(), backlinks: vec![vec![]; notes.len()], notes, by_name, files };
        let edges: Vec<(usize, usize)> = v
            .notes
            .iter()
            .enumerate()
            .flat_map(|(i, n)| n.links.iter().filter_map(|l| v.resolve(l)).filter(move |&t| t != i).map(move |t| (t, i)))
            .collect();
        edges.into_iter().for_each(|(t, i)| {
            if !v.backlinks[t].contains(&i) {
                v.backlinks[t].push(i)
            }
        });
        v
    }

    /// Resolves a link target by basename, preferring the shallowest path on collisions.
    pub fn resolve(&self, target: &str) -> Option<usize> {
        self.by_name.get(&key(target))?.iter().copied().min_by_key(|&i| self.notes[i].path.components().count())
    }

    /// Classifies a raw link target: URL, note, attachment file, or missing.
    pub fn target(&self, raw: &str) -> Target {
        if raw.contains("://") || raw.starts_with("mailto:") {
            return Target::Url(raw.into());
        }
        let raw = raw.replace("%20", " ");
        self.resolve(&raw)
            .map(Target::Note)
            .or_else(|| self.files.get(&key(&raw)).cloned().map(Target::File))
            .unwrap_or(Target::Missing)
    }

    /// Index of the note at a vault-relative path; notes are kept sorted by path.
    pub fn find(&self, path: &Path) -> Option<usize> {
        self.notes.binary_search_by(|n| n.path.as_path().cmp(path)).ok()
    }

    pub fn read(&self, note: usize) -> String {
        std::fs::read_to_string(self.root.join(&self.notes[note].path)).unwrap_or_default()
    }

    pub fn name(&self) -> String {
        self.root.file_name().map_or_else(|| self.root.display().to_string(), |n| n.to_string_lossy().into_owned())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;

    fn vault(files: &[(&str, &str)]) -> (tempfile::TempDir, Vault) {
        let dir = tempfile::tempdir().unwrap();
        files.iter().for_each(|(p, c)| {
            let p = dir.path().join(p);
            fs::create_dir_all(p.parent().unwrap()).unwrap();
            fs::write(p, c).unwrap();
        });
        let v = Vault::load(dir.path());
        (dir, v)
    }

    fn idx(v: &Vault, path: &str) -> usize {
        v.notes.iter().position(|n| n.path == Path::new(path)).unwrap()
    }

    #[test]
    fn key_strips_path_heading_block_and_extension() {
        assert_eq!(key("Dir/Some Note.md#Heading"), "some note");
        assert_eq!(key("Note^block"), "note");
        assert_eq!(key("img.PNG"), "img.png");
    }

    #[test]
    fn resolves_case_insensitive_and_prefers_shallowest() {
        let (_d, v) = vault(&[("a/b/Dup.md", ""), ("x/Dup.md", ""), ("Other.md", "")]);
        assert_eq!(v.resolve("dup"), Some(idx(&v, "x/Dup.md")));
        assert_eq!(v.resolve("OTHER"), Some(idx(&v, "Other.md")));
        assert_eq!(v.resolve("nope"), None);
    }

    #[test]
    fn builds_backlinks_without_self_or_duplicates() {
        let (_d, v) = vault(&[("A.md", "[[B]] [[B]] [[A]]"), ("B.md", "")]);
        assert_eq!(v.backlinks[idx(&v, "B.md")], vec![idx(&v, "A.md")]);
        assert!(v.backlinks[idx(&v, "A.md")].is_empty());
    }

    #[test]
    fn classifies_targets() {
        let (d, v) = vault(&[("N.md", ""), ("Images/Pic One.png", "x")]);
        assert_eq!(v.target("N"), Target::Note(idx(&v, "N.md")));
        assert_eq!(v.target("pic one.png"), Target::File(d.path().join("Images/Pic One.png")));
        assert_eq!(v.target("Pic%20One.png"), Target::File(d.path().join("Images/Pic One.png")));
        assert_eq!(v.target("https://x.dev"), Target::Url("https://x.dev".into()));
        assert_eq!(v.target("gone"), Target::Missing);
    }

    #[test]
    fn skips_obsidian_dir() {
        let (_d, v) = vault(&[(".obsidian/x.md", ""), ("n.md", "")]);
        assert_eq!(v.notes.len(), 1);
    }
}
