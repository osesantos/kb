mod md;
mod tui;
mod vault;

use std::{path::PathBuf, time::Instant};
use vault::Vault;

fn root(arg: Option<&String>) -> PathBuf {
    let p = PathBuf::from(arg.map_or(".", String::as_str));
    p.canonicalize().unwrap_or(p)
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

fn main() -> std::io::Result<()> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    match args.as_slice() {
        [cmd, rest @ ..] if cmd == "stats" => Ok(stats(root(rest.first()))),
        rest => tui::run(Vault::load(&root(rest.first()))),
    }
}
