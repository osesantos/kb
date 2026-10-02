// Package vault loads a directory of Markdown notes and derives its link index; nothing is persisted.
package vault

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
	"go.abhg.dev/goldmark/wikilink"
)

type Note struct {
	// Path is vault-relative and slash-separated.
	Path  string
	Title string
	Links []string
	Body  string
	// Lower is Body lowercased, kept so search never re-lowercases the vault per query.
	Lower    string
	Sections []Section
}

// Vault is a directory of notes plus the derived link index; rebuilt from disk, never persisted.
type Vault struct {
	Root      string
	Notes     []Note
	Backlinks [][]int
	byName    map[string][]int
	files     map[string]string
}

type TargetKind int

const (
	Missing TargetKind = iota
	NoteTarget
	FileTarget
	URLTarget
)

// Target is what a link points at once resolved: a note index, an attachment path or a URL.
type Target struct {
	Kind TargetKind
	Note int
	Path string
}

// Key normalises a link target to the case-insensitive basename Obsidian resolves by.
func Key(target string) string {
	if i := strings.IndexAny(target, "#^"); i >= 0 {
		target = target[:i]
	}
	base := target[strings.LastIndex(target, "/")+1:]
	return strings.ToLower(strings.TrimSpace(strings.TrimSuffix(base, ".md")))
}

var md = goldmark.New(goldmark.WithExtensions(&wikilink.Extender{}, extension.Table, extension.Strikethrough, extension.TaskList))

// stripFrontmatter blanks a leading YAML block so links in metadata are not counted as note links.
func stripFrontmatter(src string) string {
	if !strings.HasPrefix(src, "---\n") {
		return src
	}
	for _, end := range []string{"\n---\n", "\n...\n"} {
		if i := strings.Index(src[3:], end); i >= 0 {
			return src[3+i+len(end):]
		}
	}
	return src
}

// Wikilinks returns the targets of `[[...]]` links (not `![[...]]` embeds), fragment included.
func Wikilinks(src string) []string {
	body := []byte(stripFrontmatter(src))
	links := []string{}
	_ = ast.Walk(md.Parser().Parse(text.NewReader(body)), func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if w, ok := n.(*wikilink.Node); ok && entering && !w.Embed {
			t := string(w.Target)
			if len(w.Fragment) > 0 {
				t += "#" + string(w.Fragment)
			}
			links = append(links, t)
		}
		return ast.WalkContinue, nil
	})
	return links
}

// pathCmp orders paths component by component, as the Rust version did, so `a/x` sorts before `a b/x`.
func pathCmp(a, b string) int {
	return strings.Compare(strings.ReplaceAll(a, "/", "\x00"), strings.ReplaceAll(b, "/", "\x00"))
}

// walk lists files under root, skipping hidden entries.
// ponytail: hidden-only skip, no .gitignore parsing; every ignored file in the real vault sits in a hidden dir.
func walk(root string) (mdFiles, other []string) {
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return nil
		case p != root && strings.HasPrefix(d.Name(), "."):
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		case d.Type().IsRegular() && strings.HasSuffix(p, ".md"):
			mdFiles = append(mdFiles, p)
		case d.Type().IsRegular():
			other = append(other, p)
		}
		return nil
	})
	return mdFiles, other
}

func parse(root, p string) (Note, bool) {
	src, err := os.ReadFile(p)
	if err != nil {
		return Note{}, false
	}
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return Note{}, false
	}
	body := string(src)
	lower := strings.ToLower(body)
	rel = filepath.ToSlash(rel)
	return Note{
		Path:     rel,
		Title:    strings.TrimSuffix(filepath.Base(rel), ".md"),
		Links:    Wikilinks(body),
		Body:     body,
		Lower:    lower,
		Sections: Sections(body, lower),
	}, true
}

// Load reads every note under root in parallel and builds the name index and backlinks.
func Load(root string) *Vault {
	mdFiles, other := walk(root)
	files := make(map[string]string, len(other))
	for _, p := range other {
		files[strings.ToLower(filepath.Base(p))] = p
	}

	parsed := make([]Note, len(mdFiles))
	ok := make([]bool, len(mdFiles))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range runtime.NumCPU() {
		wg.Go(func() {
			for i := range jobs {
				parsed[i], ok[i] = parse(root, mdFiles[i])
			}
		})
	}
	for i := range mdFiles {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	notes := make([]Note, 0, len(parsed))
	for i, n := range parsed {
		if ok[i] {
			notes = append(notes, n)
		}
	}
	slices.SortFunc(notes, func(a, b Note) int { return pathCmp(a.Path, b.Path) })

	v := &Vault{Root: root, Notes: notes, Backlinks: make([][]int, len(notes)), byName: map[string][]int{}, files: files}
	for i, n := range notes {
		k := Key(n.Title)
		v.byName[k] = append(v.byName[k], i)
	}
	for i, n := range notes {
		for _, l := range n.Links {
			if t, found := v.Resolve(l); found && t != i && !slices.Contains(v.Backlinks[t], i) {
				v.Backlinks[t] = append(v.Backlinks[t], i)
			}
		}
	}
	return v
}

// Resolve finds a note by basename, preferring the shallowest path on collisions.
func (v *Vault) Resolve(target string) (int, bool) {
	cands := v.byName[Key(target)]
	if len(cands) == 0 {
		return 0, false
	}
	return slices.MinFunc(cands, func(a, b int) int {
		return strings.Count(v.Notes[a].Path, "/") - strings.Count(v.Notes[b].Path, "/")
	}), true
}

// Target classifies a raw link target: URL, note, attachment file, or missing.
func (v *Vault) Target(raw string) Target {
	if strings.Contains(raw, "://") || strings.HasPrefix(raw, "mailto:") {
		return Target{Kind: URLTarget, Path: raw}
	}
	raw = strings.ReplaceAll(raw, "%20", " ")
	if i, ok := v.Resolve(raw); ok {
		return Target{Kind: NoteTarget, Note: i}
	}
	if p, ok := v.files[Key(raw)]; ok {
		return Target{Kind: FileTarget, Path: p}
	}
	return Target{Kind: Missing}
}

// Find returns the index of the note at a vault-relative path.
func (v *Vault) Find(path string) (int, bool) {
	return slices.BinarySearchFunc(v.Notes, path, func(n Note, p string) int { return pathCmp(n.Path, p) })
}

// Read returns the note's current content on disk, or "" when it can no longer be read.
func (v *Vault) Read(note int) string {
	b, _ := os.ReadFile(filepath.Join(v.Root, v.Notes[note].Path))
	return string(b)
}

func (v *Vault) Name() string {
	return filepath.Base(v.Root)
}
