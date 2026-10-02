package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func load(t *testing.T, files map[string]string) (string, *Vault) {
	t.Helper()
	dir := t.TempDir()
	for p, c := range files {
		full := filepath.Join(dir, p)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(c), 0o644))
	}
	return dir, Load(dir)
}

func idx(t *testing.T, v *Vault, path string) int {
	t.Helper()
	i, ok := v.Find(path)
	require.True(t, ok, path)
	return i
}

func TestKeyStripsPathHeadingBlockAndExtension(t *testing.T) {
	assert.Equal(t, "some note", Key("Dir/Some Note.md#Heading"))
	assert.Equal(t, "note", Key("Note^block"))
	assert.Equal(t, "img.png", Key("img.PNG"))
}

func TestResolvesCaseInsensitiveAndPrefersShallowest(t *testing.T) {
	_, v := load(t, map[string]string{"a/b/Dup.md": "", "x/Dup.md": "", "Other.md": ""})
	i, ok := v.Resolve("dup")
	assert.True(t, ok)
	assert.Equal(t, idx(t, v, "x/Dup.md"), i)
	i, _ = v.Resolve("OTHER")
	assert.Equal(t, idx(t, v, "Other.md"), i)
	_, ok = v.Resolve("nope")
	assert.False(t, ok)
}

func TestBuildsBacklinksWithoutSelfOrDuplicates(t *testing.T) {
	_, v := load(t, map[string]string{"A.md": "[[B]] [[B]] [[A]]", "B.md": ""})
	assert.Equal(t, []int{idx(t, v, "A.md")}, v.Backlinks[idx(t, v, "B.md")])
	assert.Empty(t, v.Backlinks[idx(t, v, "A.md")])
}

func TestClassifiesTargets(t *testing.T) {
	d, v := load(t, map[string]string{"N.md": "", "Images/Pic One.png": "x"})
	pic := filepath.Join(d, "Images/Pic One.png")
	assert.Equal(t, Target{Kind: NoteTarget, Note: idx(t, v, "N.md")}, v.Target("N"))
	assert.Equal(t, Target{Kind: FileTarget, Path: pic}, v.Target("pic one.png"))
	assert.Equal(t, Target{Kind: FileTarget, Path: pic}, v.Target("Pic%20One.png"))
	assert.Equal(t, Target{Kind: URLTarget, Path: "https://x.dev"}, v.Target("https://x.dev"))
	assert.Equal(t, Target{Kind: Missing}, v.Target("gone"))
}

func TestSkipsHiddenDirs(t *testing.T) {
	_, v := load(t, map[string]string{".obsidian/x.md": "", ".hidden.md": "", "n.md": ""})
	assert.Len(t, v.Notes, 1)
}

func TestWikilinksSkipEmbedsCodeAndFrontmatter(t *testing.T) {
	src := "---\nrelated: \"[[Meta]]\"\n---\n[[A]] [[B#H|alias]] ![[img.png]] `[[code]]`"
	assert.Equal(t, []string{"A", "B#H"}, Wikilinks(src))
}

func TestOrdersPathsByComponent(t *testing.T) {
	_, v := load(t, map[string]string{"a b/x.md": "", "a/x.md": ""})
	assert.Equal(t, "a/x.md", v.Notes[0].Path)
	_, ok := v.Find("a b/x.md")
	assert.True(t, ok)
}

type sec struct {
	head       string
	start, end int
}

func secs(body string) []sec {
	out := []sec{}
	for _, s := range Sections(body, strings.ToLower(body)) {
		out = append(out, sec{s.Heading, s.Start, s.End})
	}
	return out
}

func TestSplitsAtH2H3OutsideFences(t *testing.T) {
	body := "---\ntags: x\n---\n# Title\nintro\n## A\n```\n## not a heading\n```\n#### deep\n### B\ntext"
	assert.Equal(t, []sec{{"", 1, 5}, {"A", 6, 10}, {"B", 11, 12}}, secs(body))
	assert.Equal(t, []sec{{"", 1, 1}}, secs(""))
}

func TestSplitsLongSectionsAtBlankLines(t *testing.T) {
	para := strings.Repeat("x", 4000) + "\n\n"
	s := secs("## Big\n" + para + para + para)
	assert.Greater(t, len(s), 1)
	for _, x := range s {
		assert.Equal(t, "Big", x.head)
	}
}

func TestBlockIncludesSubheadingsUntilSameLevel(t *testing.T) {
	body := "# T\n## Scope\nin\n### Sub\nmore\n## Next\nout"
	b, ok := Block(body, "scope")
	assert.True(t, ok)
	assert.Equal(t, "## Scope\nin\n### Sub\nmore\n", b)
	b, _ = Block(body, "Next")
	assert.Equal(t, "## Next\nout", b)
	_, ok = Block(body, "nope")
	assert.False(t, ok)
}
