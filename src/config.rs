//! `~/.config/kb/config.toml`: the vaults kb knows about and their per-vault overrides.

use serde::Deserialize;
use std::path::{Path, PathBuf};

#[derive(Debug, Default, Deserialize)]
pub struct Config {
    #[serde(default, rename = "vault")]
    pub vaults: Vec<VaultCfg>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct VaultCfg {
    pub name: String,
    pub path: PathBuf,
    #[serde(default)]
    pub daily: DailyCfg,
}

/// Daily-note overrides; an unset field falls back to the vault's Obsidian settings.
#[derive(Debug, Clone, Default, Deserialize)]
pub struct DailyCfg {
    pub folder: Option<String>,
    pub format: Option<String>,
}

fn home() -> PathBuf {
    std::env::var_os("HOME").map(PathBuf::from).unwrap_or_default()
}

/// Expands a leading `~` and canonicalizes, so configured paths compare equal to the opened root.
pub fn expand(p: &Path) -> PathBuf {
    let p = p.strip_prefix("~").map_or_else(|_| p.to_path_buf(), |rest| home().join(rest));
    p.canonicalize().unwrap_or(p)
}

pub fn path() -> PathBuf {
    std::env::var_os("XDG_CONFIG_HOME").map_or_else(|| home().join(".config"), PathBuf::from).join("kb/config.toml")
}

impl Config {
    /// Parses the config file; a missing file is an empty config, a malformed one is an error.
    pub fn load(file: &Path) -> Result<Config, String> {
        let src = match std::fs::read_to_string(file) {
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(Config::default()),
            r => r.map_err(|e| format!("{}: {e}", file.display()))?,
        };
        let mut c: Config = toml::from_str(&src).map_err(|e| format!("{}: {e}", file.display()))?;
        c.vaults.iter_mut().for_each(|v| v.path = expand(&v.path));
        Ok(c)
    }

    /// A configured vault's path by name, else `arg` taken as a path.
    pub fn root(&self, arg: &str) -> PathBuf {
        self.vaults.iter().find(|v| v.name == arg).map_or_else(|| expand(Path::new(arg)), |v| v.path.clone())
    }

    pub fn daily(&self, root: &Path) -> DailyCfg {
        self.vaults.iter().find(|v| v.path == root).map(|v| v.daily.clone()).unwrap_or_default()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn missing_file_is_empty_and_bad_file_errors() {
        let d = tempfile::tempdir().unwrap();
        assert!(Config::load(&d.path().join("nope.toml")).unwrap().vaults.is_empty());
        std::fs::write(d.path().join("bad.toml"), "[[vault]]\nname = 1").unwrap();
        assert!(Config::load(&d.path().join("bad.toml")).unwrap_err().contains("bad.toml"));
    }

    #[test]
    fn resolves_names_paths_and_daily_overrides() {
        let d = tempfile::tempdir().unwrap();
        let root = d.path().canonicalize().unwrap();
        let src = format!("[[vault]]\nname = \"work\"\npath = \"{}\"\n[vault.daily]\nformat = \"YYYY-MM-DD\"\n", root.display());
        std::fs::write(root.join("c.toml"), src).unwrap();
        let c = Config::load(&root.join("c.toml")).unwrap();
        assert_eq!(c.root("work"), root);
        assert_eq!(c.root(root.to_str().unwrap()), root);
        assert_eq!(c.daily(&root).format.as_deref(), Some("YYYY-MM-DD"));
        assert_eq!(c.daily(&root).folder, None);
        assert_eq!(c.daily(Path::new("/elsewhere")).format, None);
    }
}
