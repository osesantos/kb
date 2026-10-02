package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/osesantos/kb/internal/vault"
)

// names flattens a tree depth-first as "name" with two spaces per level.
func names(nodes []noteNode, depth int) []string {
	out := []string{}
	for _, n := range nodes {
		out = append(out, indent(depth)+n.Item.name)
		out = append(out, names(n.Children, depth+1)...)
	}
	return out
}

func indent(d int) string {
	s := ""
	for range d {
		s += "  "
	}
	return s
}

func TestBuildNoteTreeFoldersFirstCaseInsensitive(t *testing.T) {
	paths := []string{"b.md", "A.md", "zeta/x.md", "Alpha/c.md", "Alpha/sub/d.md", "alpha2/e.md"}
	notes := make([]vault.Note, len(paths))
	all := make([]int, len(paths))
	for i, p := range paths {
		notes[i] = vault.Note{Path: p, Title: baseTitle(p)}
		all[i] = i
	}
	cases := []struct {
		name    string
		include []int
		want    []string
	}{
		{"all", all, []string{"Alpha", "  sub", "    d", "  c", "alpha2", "  e", "zeta", "  x", "A", "b"}},
		{"subset keeps only the folders of included notes", []int{0, 4}, []string{"Alpha", "  sub", "    d", "b"}},
		{"none", nil, []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { assert.Equal(t, c.want, names(buildNoteTree(notes, c.include), 0)) })
	}
}

func baseTitle(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			p = p[i+1:]
			break
		}
	}
	return p[:len(p)-3]
}
