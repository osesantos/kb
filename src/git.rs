//! Thin wrappers over the `git` CLI so signing, hooks and credential helpers behave as configured.

use std::{
    path::{Path, PathBuf},
    process::{Command, Stdio},
};

#[derive(Debug, Clone, PartialEq)]
pub struct Entry {
    pub x: char,
    pub y: char,
    /// Path relative to the repository top level.
    pub path: String,
}

impl Entry {
    pub fn untracked(&self) -> bool {
        self.x == '?'
    }

    /// Fully staged: something in the index and nothing left in the worktree.
    pub fn staged(&self) -> bool {
        !matches!(self.x, ' ' | '?') && self.y == ' '
    }
}

fn git(root: &Path, args: &[&str], ok: &[i32]) -> Result<String, String> {
    let out = Command::new("git")
        .arg("-C")
        .arg(root)
        .args(args)
        .env("GIT_TERMINAL_PROMPT", "0")
        .stdin(Stdio::null())
        .output()
        .map_err(|e| format!("git: {e}"))?;
    match out.status.code() {
        Some(c) if ok.contains(&c) => Ok(String::from_utf8_lossy(&out.stdout).into_owned()),
        _ => Err(String::from_utf8_lossy(&out.stderr).lines().find(|l| !l.trim().is_empty()).unwrap_or("git failed").trim().into()),
    }
}

fn top_path(path: &str) -> String {
    format!(":(top,literal){path}")
}

/// Parses `git status --porcelain=v1 -z`; rename/copy records carry an extra source path that is skipped.
pub fn parse(out: &str) -> Vec<Entry> {
    let mut recs = out.split('\0').filter(|r| r.len() > 3);
    std::iter::from_fn(|| {
        let r = recs.next()?;
        let mut cs = r.chars();
        let (x, y) = (cs.next()?, cs.next()?);
        if matches!(x, 'R' | 'C') {
            recs.next();
        }
        Some(Entry { x, y, path: r[3..].into() })
    })
    .collect()
}

pub fn toplevel(root: &Path) -> Option<PathBuf> {
    git(root, &["rev-parse", "--show-toplevel"], &[0]).ok().map(|s| PathBuf::from(s.trim()))
}

/// Changes under `root` only, so a vault inside a larger repo shows just its own files.
pub fn status(root: &Path) -> Result<Vec<Entry>, String> {
    git(root, &["status", "--porcelain=v1", "-z", "--untracked-files=all", "--", "."], &[0]).map(|s| parse(&s))
}

pub fn diff(root: &Path, e: &Entry) -> Result<String, String> {
    let p = top_path(&e.path);
    if e.untracked() {
        let top = toplevel(root).unwrap_or_else(|| root.into());
        return git(&top, &["diff", "--no-color", "--no-index", "--", "/dev/null", &e.path], &[0, 1]);
    }
    let staged = git(root, &["diff", "--no-color", "--cached", "--", &p], &[0])?;
    let unstaged = git(root, &["diff", "--no-color", "--", &p], &[0])?;
    Ok(staged + &unstaged)
}

pub fn stage(root: &Path, path: &str) -> Result<String, String> {
    git(root, &["add", "-A", "--", &top_path(path)], &[0])
}

pub fn stage_all(root: &Path) -> Result<String, String> {
    git(root, &["add", "-A", "--", "."], &[0])
}

pub fn unstage(root: &Path, path: &str) -> Result<String, String> {
    git(root, &["reset", "-q", "--", &top_path(path)], &[0])
}

pub fn commit(root: &Path, msg: &str) -> Result<String, String> {
    git(root, &["commit", "-q", "-m", msg], &[0]).and_then(|_| git(root, &["log", "-1", "--format=%h %s"], &[0]))
}

pub fn push(root: &Path) -> Result<String, String> {
    git(root, &["push", "-q"], &[0])
}

#[cfg(test)]
pub fn init(dir: &Path) {
    [&["init", "-q"][..], &["config", "user.email", "t@t"], &["config", "user.name", "t"], &["config", "commit.gpgsign", "false"]]
        .iter()
        .for_each(|a| {
            git(dir, a, &[0]).unwrap();
        });
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_porcelain_z_including_renames() {
        let e = parse(" M a b.md\0R  new.md\0old.md\0?? x.md\0");
        assert_eq!(e.iter().map(|e| (e.x, e.y, e.path.as_str())).collect::<Vec<_>>(), [(' ', 'M', "a b.md"), ('R', ' ', "new.md"), ('?', '?', "x.md")]);
        assert!(e[1].staged() && !e[0].staged() && e[2].untracked());
    }

    #[test]
    fn stage_commit_cycle() {
        let d = tempfile::tempdir().unwrap();
        init(d.path());
        std::fs::write(d.path().join("n.md"), "hi\n").unwrap();
        let e = &status(d.path()).unwrap()[0];
        assert!(e.untracked());
        assert!(diff(d.path(), e).unwrap().contains("+hi"));
        stage(d.path(), &e.path).unwrap();
        assert!(status(d.path()).unwrap()[0].staged());
        unstage(d.path(), "n.md").unwrap_or_default();
        stage_all(d.path()).unwrap();
        assert!(commit(d.path(), "first").unwrap().ends_with("first\n"));
        assert!(status(d.path()).unwrap().is_empty());
        assert!(push(d.path()).is_err());
    }
}
