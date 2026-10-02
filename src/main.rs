mod config;
mod daily;
mod git;
mod md;
mod search;
mod tui;
mod vault;

use config::Config;
use std::{path::PathBuf, time::Instant};
use vault::Vault;

/// Vault to open: the argument, else `KB_VAULT`, else the first configured vault, else `.`; names resolve via config.
fn root(cfg: &Config, arg: Option<&String>) -> PathBuf {
    let arg = arg.cloned().or_else(|| std::env::var("KB_VAULT").ok());
    match (arg, cfg.vaults.first()) {
        (Some(a), _) => cfg.root(&a),
        (None, Some(v)) => v.path.clone(),
        (None, None) => cfg.root("."),
    }
}

fn stats(root: PathBuf) {
    let t = Instant::now();
    let v = Vault::load(&root);
    let took = t.elapsed();
    let links: Vec<&String> = v.notes.iter().flat_map(|n| &n.links).collect();
    let unresolved = links.iter().filter(|l| v.target(l) == vault::Target::Missing).count();
    println!("notes       {}", v.notes.len());
    println!("load        {took:?}");
    println!("wikilinks   {}", links.len());
    println!("unresolved  {unresolved}");
}

/// `kb search [-C vault] [--json] [-n limit] words...`: grep-like lines by default, JSON for agents.
fn search(cfg: &Config, args: &[String]) -> std::io::Result<()> {
    let (mut vault, mut json, mut limit, mut words) = (None, false, 20, vec![]);
    let mut it = args.iter();
    while let Some(a) = it.next() {
        match a.as_str() {
            "--json" => json = true,
            "-C" | "--vault" => vault = it.next(),
            "-n" => limit = it.next().and_then(|n| n.parse().ok()).unwrap_or(limit),
            w => words.push(w),
        }
    }
    let v = Vault::load(&root(cfg, vault));
    let hits = search::lexical(&v, &words.join(" "), limit);
    if json {
        println!("{}", search::json(&v, &hits));
    } else {
        hits.iter().for_each(|h| match &h.line {
            Some((n, l)) => println!("{}:{n}: {l}", v.notes[h.note].path.display()),
            None => println!("{}", v.notes[h.note].path.display()),
        });
    }
    if hits.is_empty() { std::process::exit(1) }
    Ok(())
}

fn main() -> std::io::Result<()> {
    let cfg = Config::load(&config::path()).unwrap_or_else(|e| {
        eprintln!("kb: {e}");
        std::process::exit(2)
    });
    let args: Vec<String> = std::env::args().skip(1).collect();
    match args.as_slice() {
        [cmd, rest @ ..] if cmd == "stats" => {
            stats(root(&cfg, rest.first()));
            Ok(())
        }
        [cmd, rest @ ..] if cmd == "search" => search(&cfg, rest),
        rest => tui::run(Vault::load(&root(&cfg, rest.first())), cfg),
    }
}
