package tui

import (
	"cmp"
	"slices"
	"strings"

	"github.com/osesantos/kb/internal/tui/components"
	"github.com/osesantos/kb/internal/vault"
)

// treeItem is one row of the notes tree: a folder (note < 0) or a note index.
type treeItem struct {
	name string
	note int
}

type noteNode = components.TreeNode[treeItem]

// buildNoteTree arranges the given notes into folders by path: folders before files, each A→Z case-insensitive.
// IDs are vault paths, so a folder is "a/b" and a note "a/b/n.md".
func buildNoteTree(notes []vault.Note, include []int) []noteNode {
	type dir struct {
		dirs  map[string]*dir
		files []noteNode
	}
	root := &dir{dirs: map[string]*dir{}}
	for _, i := range include {
		parts := strings.Split(notes[i].Path, "/")
		d := root
		for _, p := range parts[:len(parts)-1] {
			if d.dirs[p] == nil {
				d.dirs[p] = &dir{dirs: map[string]*dir{}}
			}
			d = d.dirs[p]
		}
		d.files = append(d.files, noteNode{ID: notes[i].Path, Item: treeItem{name: notes[i].Title, note: i}})
	}
	byName := func(a, b noteNode) int {
		return cmp.Or(cmp.Compare(strings.ToLower(a.Item.name), strings.ToLower(b.Item.name)), cmp.Compare(a.ID, b.ID))
	}
	var build func(d *dir, prefix string) []noteNode
	build = func(d *dir, prefix string) []noteNode {
		folders := make([]noteNode, 0, len(d.dirs))
		for name, sub := range d.dirs {
			id := prefix + name
			folders = append(folders, noteNode{ID: id, Item: treeItem{name: name, note: -1}, Children: build(sub, id+"/")})
		}
		slices.SortFunc(folders, byName)
		slices.SortFunc(d.files, byName)
		return append(folders, d.files...)
	}
	return build(root, "")
}
