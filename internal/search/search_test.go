package search

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/osesantos/kb/internal/vault"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func load(t *testing.T, files map[string]string) *vault.Vault {
	t.Helper()
	d := t.TempDir()
	for p, c := range files {
		require.NoError(t, os.WriteFile(filepath.Join(d, p), []byte(c), 0o644))
	}
	return vault.Load(d)
}

func base(t *testing.T) *vault.Vault {
	return load(t, map[string]string{
		"Agent Memory.md": "# Agent memory\n\nstored in qdrant",
		"Other.md":        "intro\n## Design\nthe agent memory design\nmemory memory\n## Later\nunrelated agent",
		"None.md":         "nothing",
	})
}

func TestCountsFromWordStartsOnly(t *testing.T) {
	assert.Equal(t, 2, count("commit commits recommit", "commit"))
}

func TestRequiresAllWordsAndRanksBestSectionFirst(t *testing.T) {
	v := base(t)
	hits := Lexical(v, "Agent MEMORY", 10)
	require.GreaterOrEqual(t, len(hits), 2)
	got := [][2]string{}
	for _, h := range hits[:2] {
		got = append(got, [2]string{v.Notes[h.Note].Title, v.Notes[h.Note].Sections[h.Section].Heading})
	}
	assert.Equal(t, [][2]string{{"Agent Memory", ""}, {"Other", "Design"}}, got)
	assert.Equal(t, 3, hits[1].Line)
	assert.Equal(t, "the agent memory design", hits[1].Snippet)
	assert.Empty(t, Lexical(v, "agent nope", 10))
	assert.Empty(t, Lexical(v, "  ", 10))
}

func TestShorterSectionWinsOnEqualMatches(t *testing.T) {
	v := load(t, map[string]string{"N.md": "## Long\nzeta " + strings.Repeat("filler ", 200) + "\n## Short\nzeta\n"})
	hits := Lexical(v, "zeta", 10)
	assert.Equal(t, "Short", v.Notes[0].Sections[hits[0].Section].Heading)
}

func parse(t *testing.T, s string) []map[string]any {
	t.Helper()
	var out []map[string]any
	require.NoError(t, json.Unmarshal([]byte(s), &out))
	return out
}

func TestJSONHasContractFieldsAndRespectsBudget(t *testing.T) {
	v := base(t)
	hits := Lexical(v, "agent", 10)
	j := parse(t, JSON(v, hits, nil))
	assert.Equal(t, "lexical", j[0]["provider"])
	assert.NotNil(t, j[0]["line_start"])
	assert.NotNil(t, j[0]["tokens"])
	assert.NotContains(t, j[0], "text")

	q := parse(t, JSON(v, Lexical(v, "qdrant", 10), nil))
	assert.Equal(t, "Agent Memory.md", q[0]["path"])
	assert.EqualValues(t, 3, q[0]["line"])

	first := int(j[0]["tokens"].(float64))
	b := parse(t, JSON(v, hits, &first))
	assert.IsType(t, "", b[0]["text"])
	for _, h := range b[1:] {
		if _, ok := h["text"]; ok {
			assert.EqualValues(t, 0, h["tokens"])
		}
	}
}
