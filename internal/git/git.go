// Package git wraps the git CLI so signing, hooks and credential helpers behave as the user configured them.
package git

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Entry is one `git status` line: index (X) and worktree (Y) state of a path.
type Entry struct {
	X, Y byte
	// Path is relative to the repository top level.
	Path string
}

// Untracked reports a path git does not know yet.
func (e Entry) Untracked() bool { return e.X == '?' }

// Staged reports something in the index and nothing left in the worktree.
func (e Entry) Staged() bool { return e.X != ' ' && e.X != '?' && e.Y == ' ' }

// run executes git in root and returns stdout when the exit code is in ok, else the first stderr line.
func run(root string, ok []int, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err := cmd.Run()
	code := 0
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		code = exit.ExitCode()
	case err != nil:
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	if slices.Contains(ok, code) {
		return out.String(), nil
	}
	for l := range strings.SplitSeq(stderr.String(), "\n") {
		if strings.TrimSpace(l) != "" {
			return "", fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(l))
		}
	}
	return "", fmt.Errorf("git %s: exit %d", args[0], code)
}

func topPath(p string) string { return ":(top,literal)" + p }

// Parse reads `git status --porcelain=v1 -z`; rename and copy records carry an extra source path that is skipped.
func Parse(out string) []Entry {
	entries := []Entry{}
	recs := strings.Split(out, "\x00")
	for i := 0; i < len(recs); i++ {
		r := recs[i]
		if len(r) <= 3 {
			continue
		}
		entries = append(entries, Entry{X: r[0], Y: r[1], Path: r[3:]})
		if r[0] == 'R' || r[0] == 'C' {
			i++
		}
	}
	return entries
}

// Toplevel is the repository root containing root, if any.
func Toplevel(root string) (string, bool) {
	out, err := run(root, []int{0}, "rev-parse", "--show-toplevel")
	return strings.TrimSpace(out), err == nil
}

// Status lists changes under root only, so a vault inside a larger repo shows just its own files.
func Status(root string) ([]Entry, error) {
	out, err := run(root, []int{0}, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--", ".")
	return Parse(out), err
}

// Diff is the staged plus unstaged diff of an entry; untracked files diff against /dev/null.
func Diff(root string, e Entry) (string, error) {
	if e.Untracked() {
		top, ok := Toplevel(root)
		if !ok {
			top = root
		}
		return run(top, []int{0, 1}, "diff", "--no-color", "--no-index", "--", "/dev/null", e.Path)
	}
	staged, err := run(root, []int{0}, "diff", "--no-color", "--cached", "--", topPath(e.Path))
	if err != nil {
		return "", err
	}
	unstaged, err := run(root, []int{0}, "diff", "--no-color", "--", topPath(e.Path))
	return staged + unstaged, err
}

// Stage adds a path (or its deletion) to the index.
func Stage(root, path string) (string, error) {
	return run(root, []int{0}, "add", "-A", "--", topPath(path))
}

// StageAll stages every change under root.
func StageAll(root string) (string, error) { return run(root, []int{0}, "add", "-A", "--", ".") }

// Unstage removes a path from the index, keeping the worktree.
func Unstage(root, path string) (string, error) {
	return run(root, []int{0}, "reset", "-q", "--", topPath(path))
}

// Commit commits the index and returns the new commit's short hash and subject.
func Commit(root, msg string) (string, error) {
	if _, err := run(root, []int{0}, "commit", "-q", "-m", msg); err != nil {
		return "", err
	}
	return run(root, []int{0}, "log", "-1", "--format=%h %s")
}

// Push pushes the current branch; it never prompts, so a missing credential fails fast.
func Push(root string) (string, error) { return run(root, []int{0}, "push", "-q") }

// Pull merges the upstream branch; a conflict is not an error, it leaves the repository Merging.
func Pull(root string) (string, error) {
	out, err := run(root, []int{0}, "pull", "--no-rebase", "--no-edit", "-q")
	if err != nil && Merging(root) {
		return out, nil
	}
	return out, err
}

// HasUpstream reports a repository whose current branch tracks a remote branch.
func HasUpstream(root string) bool {
	_, err := run(root, []int{0}, "rev-parse", "-q", "--verify", "@{upstream}")
	return err == nil
}

// Merging reports an unfinished merge (MERGE_HEAD exists).
func Merging(root string) bool {
	_, err := run(root, []int{0}, "rev-parse", "-q", "--verify", "MERGE_HEAD")
	return err == nil
}

// Conflicts lists unmerged paths, relative to the repository top level.
func Conflicts(root string) ([]string, error) {
	out, err := run(root, []int{0}, "-c", "core.quotePath=false", "diff", "--name-only", "--diff-filter=U", "-z")
	return slices.DeleteFunc(strings.Split(out, "\x00"), func(p string) bool { return p == "" }), err
}

// HasMarkers reports whether text still holds a conflict marker at the start of a line.
func HasMarkers(text string) bool {
	return slices.ContainsFunc(strings.Split(text, "\n"), func(l string) bool {
		return strings.HasPrefix(l, "<<<<<<< ") || strings.HasPrefix(l, ">>>>>>> ") || l == "======="
	})
}

// FinishMerge stages the resolved paths and commits the merge with git's prepared message.
func FinishMerge(root string, paths []string) (string, error) {
	for _, p := range paths {
		if _, err := Stage(root, p); err != nil {
			return "", err
		}
	}
	if _, err := run(root, []int{0}, "commit", "-q", "--no-edit"); err != nil {
		return "", err
	}
	return run(root, []int{0}, "log", "-1", "--format=%h %s")
}

// LogEntry is a commit touching the vault, with its `Agent:` / `Run:` trailers when the writer set them.
type LogEntry struct {
	Hash, Author, Agent, Run, Subject string
	At                                int64
}

const logFormat = "--format=%h%x1f%at%x1f%an%x1f%(trailers:key=Agent,valueonly,separator=%x2C)%x1f%(trailers:key=Run,valueonly,separator=%x2C)%x1f%s%x1e"

// ParseLog reads records written with logFormat.
func ParseLog(out string) []LogEntry {
	entries := []LogEntry{}
	for r := range strings.SplitSeq(out, "\x1e") {
		f := strings.Split(strings.TrimLeft(r, "\n"), "\x1f")
		if len(f) != 6 {
			continue
		}
		at, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil {
			continue
		}
		entries = append(entries, LogEntry{Hash: f[0], At: at, Author: f[2], Agent: strings.TrimSpace(f[3]), Run: strings.TrimSpace(f[4]), Subject: f[5]})
	}
	return entries
}

// Log is the latest n commits touching files under root, newest first; an unborn branch has none.
func Log(root string, n int) ([]LogEntry, error) {
	if _, ok := Toplevel(root); !ok {
		return nil, errors.New("not a git repository")
	}
	if _, err := run(root, []int{0}, "rev-parse", "-q", "--verify", "HEAD"); err != nil {
		return nil, nil
	}
	out, err := run(root, []int{0}, "log", "-n", strconv.Itoa(n), logFormat, "--", ".")
	return ParseLog(out), err
}

// Created maps each Markdown file under root first added between from and to (paths relative to root) to that commit's time.
// Renames count as additions, so a renamed note dates from its rename.
func Created(root string, from, to time.Time) (map[string]time.Time, error) {
	if _, ok := Toplevel(root); !ok {
		return nil, errors.New("not a git repository")
	}
	if _, err := run(root, []int{0}, "rev-parse", "-q", "--verify", "HEAD"); err != nil {
		return map[string]time.Time{}, nil
	}
	out, err := run(root, []int{0}, "-c", "core.quotePath=false", "log", "--no-renames", "--diff-filter=A", "--relative",
		"--since="+from.Format(time.RFC3339), "--until="+to.Format(time.RFC3339),
		"--format=%x1e%at", "--name-only", "--", "*.md")
	if err != nil {
		return nil, err
	}
	created := map[string]time.Time{}
	for rec := range strings.SplitSeq(out, "\x1e") {
		lines := slices.DeleteFunc(strings.Split(rec, "\n"), func(l string) bool { return l == "" })
		if len(lines) < 2 {
			continue
		}
		at, err := strconv.ParseInt(lines[0], 10, 64)
		if err != nil {
			continue
		}
		for _, p := range lines[1:] {
			if t, seen := created[p]; !seen || time.Unix(at, 0).Before(t) {
				created[p] = time.Unix(at, 0)
			}
		}
	}
	return created, nil
}

// Show is the stat and patch of one commit, limited to files under root.
func Show(root, hash string) (string, error) {
	return run(root, []int{0}, "show", "--no-color", "--stat", "--patch", "--format=%H%n%an  %ad%n%n%B", hash, "--", ".")
}
