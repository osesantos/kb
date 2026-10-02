package components

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func sample() []TreeNode[string] {
	return []TreeNode[string]{
		{ID: "a", Item: "a", Children: []TreeNode[string]{
			{ID: "a/b", Item: "b", Children: []TreeNode[string]{{ID: "a/b/n.md", Item: "n"}}},
		}},
		{ID: "z.md", Item: "z"},
	}
}

func TestRevealExpandsAncestorsAndSelects(t *testing.T) {
	tr := NewTree(func(item string, _, _, _ int, _, _, _ bool) string { return item }).SetNodes(sample())
	assert.Equal(t, 2, tr.RowCount(), "starts collapsed")
	tr = tr.Reveal("a/b/n.md")
	assert.Equal(t, "a/b/n.md", tr.SelectedID())
	assert.Equal(t, 4, tr.RowCount())
	assert.Equal(t, "a/b", tr.ParentID("a/b/n.md"))
	assert.Equal(t, "", tr.ParentID("a"))
}

func TestExpansionSnapshotRestores(t *testing.T) {
	tr := NewTree(func(item string, _, _, _ int, _, _, _ bool) string { return item }).SetNodes(sample())
	saved := tr.Expansion()
	tr = tr.ExpandAll()
	assert.Equal(t, 4, tr.RowCount())
	tr = tr.WithExpansion(saved)
	assert.Equal(t, 2, tr.RowCount())
	tr = tr.SetExpanded("a", true)
	_, depth, kids, open, ok := tr.SelectIndex(1).Current()
	assert.True(t, ok)
	assert.Equal(t, []any{1, true, false}, []any{depth, kids, open})
}
