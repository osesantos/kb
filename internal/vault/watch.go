package vault

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Relevant reports whether a changed path can affect notes or attachments (git and Obsidian internals can't).
func Relevant(p string) bool {
	for part := range strings.SplitSeq(filepath.ToSlash(p), "/") {
		if part == ".git" || part == ".obsidian" {
			return false
		}
	}
	return true
}

// InGit reports whether a changed path is inside a .git directory, i.e. the repository state moved.
func InGit(p string) bool {
	return strings.Contains(filepath.ToSlash(p), "/.git/") || filepath.Base(p) == ".git"
}

// Watcher batches file changes under a vault root.
type Watcher struct {
	// Batches delivers the changed paths, coalesced until debounce passes with no new event.
	Batches <-chan []string
	// Unwatched lists directories that could not be watched at start; changes there do not refresh live.
	Unwatched []string
	w         *fsnotify.Watcher
}

// Close stops the watcher and closes Batches.
func (w *Watcher) Close() error { return w.w.Close() }

// addTree watches dir and every non-hidden directory below it (fsnotify is not recursive) and returns the ones it could not watch.
func addTree(w *fsnotify.Watcher, dir string) []string {
	failed := []string{}
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error { // the callback never returns an error
		switch {
		case err != nil || !d.IsDir():
			return nil
		case p != dir && strings.HasPrefix(d.Name(), "."):
			return filepath.SkipDir
		}
		if w.Add(p) != nil {
			failed = append(failed, p)
		}
		return nil
	})
	return failed
}

// Watch starts watching root (plus its top-level .git, so commits are seen) and returns the batching watcher.
func Watch(root string, debounce time.Duration) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	unwatched := addTree(fw, root)
	if st, err := os.Stat(filepath.Join(root, ".git")); err == nil && st.IsDir() && fw.Add(filepath.Join(root, ".git")) != nil {
		unwatched = append(unwatched, filepath.Join(root, ".git"))
	}
	out := make(chan []string)
	go func() {
		defer close(out)
		pending := map[string]bool{}
		timer := time.NewTimer(debounce)
		timer.Stop()
		for {
			select {
			case ev, ok := <-fw.Events:
				if !ok {
					return
				}
				pending[ev.Name] = true
				if ev.Has(fsnotify.Create) && Relevant(ev.Name) {
					if st, err := os.Stat(ev.Name); err == nil && st.IsDir() && len(addTree(fw, ev.Name)) > 0 {
						pending[root] = true
					}
				}
				timer.Reset(debounce)
			case _, ok := <-fw.Errors:
				if !ok {
					return
				}
				// ponytail: a watcher error (e.g. queue overflow) is reported as a root change, which triggers a full reload
				pending[root] = true
				timer.Reset(debounce)
			case <-timer.C:
				batch := make([]string, 0, len(pending))
				for p := range pending {
					batch = append(batch, p)
				}
				pending = map[string]bool{}
				out <- batch
			}
		}
	}()
	return &Watcher{Batches: out, Unwatched: unwatched, w: fw}, nil
}
