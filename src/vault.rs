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
}

/// A directory of Markdown notes plus the derived link index; rebuilt from disk, never persisted.
pub struct Vault {
    pub root: PathBuf,
    pub notes: Vec<Note>,
    pub backlinks: Vec<Vec<usize>>,
    by_name: HashMap<String, Vec<usize>>,
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
        let mut notes: Vec<Note> = ignore::WalkBuilder::new(root)
            .filter_entry(|e| e.file_name() != ".obsidian")
            .build()
            .filter_map(Result::ok)
            .map(|e| e.into_path())
            .filter(|p| p.extension().is_some_and(|x| x == "md"))
            .collect::<Vec<_>>()
            .into_par_iter()
            .filter_map(|p| {
                let src = std::fs::read_to_string(&p).ok()?;
                let path = p.strip_prefix(root).ok()?.to_path_buf();
                Some(Note { title: path.file_stem()?.to_string_lossy().into_owned(), links: wikilinks(&src), path })
            })
            .collect();
        notes.sort_by(|a, b| a.path.cmp(&b.path));

        let by_name = notes.iter().enumerate().fold(HashMap::<_, Vec<_>>::new(), |mut m, (i, n)| {
            m.entry(key(&n.title)).or_default().push(i);
            m
        });
        let mut v = Vault { root: root.to_path_buf(), backlinks: vec![vec![]; notes.len()], notes, by_name };
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

    pub fn read(&self, note: usize) -> String {
        std::fs::read_to_string(self.root.join(&self.notes[note].path)).unwrap_or_default()
    }

    pub fn name(&self) -> String {
        self.root.file_name().map_or_else(|| self.root.display().to_string(), |n| n.to_string_lossy().into_owned())
    }
}
