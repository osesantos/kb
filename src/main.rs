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

/// `kb search [-C vault] [--json] [-n limit] [--budget tokens] words...`: grep-like lines by default, JSON for agents.
fn search(cfg: &Config, args: &[String]) -> std::io::Result<()> {
    let (mut vault, mut json, mut limit, mut budget, mut words) = (None, false, 20, None, vec![]);
    let mut it = args.iter();
    while let Some(a) = it.next() {
        match a.as_str() {
            "--json" => json = true,
            "-C" | "--vault" => vault = it.next(),
            "-n" => limit = it.next().and_then(|n| n.parse().ok()).unwrap_or(limit),
            "--budget" => budget = it.next().and_then(|n| n.parse().ok()),
            w => words.push(w),
        }
    }
    let v = Vault::load(&root(cfg, vault));
    let hits = search::lexical(&v, &words.join(" "), limit);
    if json {
        println!("{}", search::json(&v, &hits, budget));
    } else {
        hits.iter().for_each(|h| match &h.line {
            Some((n, l)) => println!("{}:{n}: {l}", v.notes[h.note].path.display()),
            None => println!("{}", v.notes[h.note].path.display()),
        });
    }
    if hits.is_empty() { std::process::exit(1) }
    Ok(())
}

/// `kb read [-C vault] <note>[#heading]`: prints a note, or only the block under one heading.
fn read(cfg: &Config, args: &[String]) {
    let (vault, target) = match args {
        [flag, v, t] if flag == "-C" || flag == "--vault" => (Some(v), t.as_str()),
        [t] => (None, t.as_str()),
        _ => {
            eprintln!("usage: kb read [-C vault] <note>[#heading]");
            std::process::exit(2)
        }
    };
    let v = Vault::load(&root(cfg, vault));
    let Some(i) = v.resolve(target) else {
        eprintln!("kb: no note {target}");
        std::process::exit(1)
    };
    let body = &v.notes[i].body;
    match target.split_once('#') {
        None => print!("{body}"),
        Some((_, h)) => match search::block(body, h) {
            Some(b) => print!("{b}"),
            None => {
                eprintln!("kb: no heading \"{h}\" in {}", v.notes[i].path.display());
                std::process::exit(1)
            }
        },
    }
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
        [cmd, rest @ ..] if cmd == "read" => {
            read(&cfg, rest);
            Ok(())
        }
        rest => tui::run(Vault::load(&root(&cfg, rest.first())), cfg),
    }
}
