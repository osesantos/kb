//! Step 0 spike: measure parse time and wikilink resolution on a real vault.

use pulldown_cmark::{Event, LinkType, Options, Parser, Tag};
use rayon::prelude::*;
use std::{collections::HashMap, path::PathBuf, time::Instant};

struct Note {
    path: PathBuf,
    links: Vec<String>,
}

fn key(target: &str) -> String {
    let t = target.split(['#', '^']).next().unwrap_or("");
    let base = t.rsplit('/').next().unwrap_or(t);
    base.strip_suffix(".md").unwrap_or(base).trim().to_lowercase()
}

fn wikilinks(src: &str) -> Vec<String> {
    Parser::new_ext(src, Options::ENABLE_WIKILINKS | Options::ENABLE_YAML_STYLE_METADATA_BLOCKS)
        .filter_map(|e| match e {
            Event::Start(Tag::Link { link_type: LinkType::WikiLink { .. }, dest_url, .. }) => Some(dest_url.to_string()),
            _ => None,
        })
        .collect()
}

/// Matches the Obsidian daily format `MMM Do, YYYY`, e.g. `Sep 29th, 2026`.
fn is_daily(stem: &str) -> bool {
    const MONTHS: [&str; 12] = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
    let mut parts = stem.split(' ');
    let (Some(m), Some(d), Some(y), None) = (parts.next(), parts.next(), parts.next(), parts.next()) else { return false };
    let day = d.trim_end_matches(',').trim_end_matches(|c: char| c.is_ascii_alphabetic());
    MONTHS.contains(&m) && d.ends_with(',') && day.parse::<u8>().is_ok_and(|n| (1..=31).contains(&n)) && y.len() == 4 && y.parse::<u16>().is_ok()
}

fn main() {
    let root = PathBuf::from(std::env::args().nth(1).unwrap_or_else(|| ".".into()));

    let t0 = Instant::now();
    let paths: Vec<PathBuf> = ignore::WalkBuilder::new(&root)
        .filter_entry(|e| e.file_name() != ".obsidian")
        .build()
        .filter_map(Result::ok)
        .map(|e| e.into_path())
        .filter(|p| p.extension().is_some_and(|x| x == "md"))
        .collect();
    let t_walk = t0.elapsed();

    let t1 = Instant::now();
    let notes: Vec<Note> = paths
        .into_par_iter()
        .filter_map(|path| std::fs::read_to_string(&path).ok().map(|s| Note { links: wikilinks(&s), path }))
        .collect();
    let t_parse = t1.elapsed();

    let t2 = Instant::now();
    let by_name = notes.iter().fold(HashMap::<String, Vec<&PathBuf>>::new(), |mut m, n| {
        m.entry(key(&n.path.file_stem().unwrap().to_string_lossy())).or_default().push(&n.path);
        m
    });
    let links: Vec<(&str, usize)> = notes
        .iter()
        .flat_map(|n| n.links.iter().map(|l| (l.as_str(), by_name.get(&key(l)).map_or(0, Vec::len))))
        .collect();
    let t_resolve = t2.elapsed();

    let total = links.len();
    let count = |f: fn(usize) -> bool| links.iter().filter(|(_, c)| f(*c)).count();
    let (unresolved, ambiguous) = (count(|c| c == 0), count(|c| c > 1));
    let pct = |n: usize| 100.0 * n as f64 / total.max(1) as f64;

    let mut dup_names: Vec<_> = by_name.iter().filter(|(_, v)| v.len() > 1).collect();
    dup_names.sort_by_key(|(_, v)| std::cmp::Reverse(v.len()));

    let mut missing = links.iter().filter(|(_, c)| *c == 0).fold(HashMap::<String, usize>::new(), |mut m, (l, _)| {
        *m.entry(key(l)).or_default() += 1;
        m
    }).into_iter().collect::<Vec<_>>();
    missing.sort_by_key(|(_, n)| std::cmp::Reverse(*n));

    let daily = notes.iter().filter(|n| is_daily(&n.path.file_stem().unwrap().to_string_lossy())).count();

    println!("notes            {}", notes.len());
    println!("walk             {t_walk:?}");
    println!("parse (rayon)    {t_parse:?}");
    println!("resolve          {t_resolve:?}");
    println!("total            {:?}", t0.elapsed());
    println!("wikilinks        {total}");
    println!("unresolved       {unresolved} ({:.1}%)", pct(unresolved));
    println!("ambiguous        {ambiguous} ({:.1}%)", pct(ambiguous));
    println!("duplicate names  {}", dup_names.len());
    println!("daily notes      {daily}");
    println!("\ntop duplicate basenames:");
    dup_names.iter().take(10).for_each(|(k, v)| println!("  {:3}  {k}", v.len()));
    println!("\ntop unresolved targets:");
    missing.iter().take(10).for_each(|(k, n)| println!("  {n:3}  {k}"));
}
