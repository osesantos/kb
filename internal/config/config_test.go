package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMissingFileIsEmptyAndBadFileErrors(t *testing.T) {
	d := t.TempDir()
	c, err := Load(filepath.Join(d, "nope.toml"))
	require.NoError(t, err)
	assert.Empty(t, c.Vaults)
	bad := filepath.Join(d, "bad.toml")
	require.NoError(t, os.WriteFile(bad, []byte("[[vault]]\nname = 1"), 0o644))
	_, err = Load(bad)
	assert.ErrorContains(t, err, "bad.toml")
}

func TestResolvesNamesPathsDailyOverridesAndTheme(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	src := fmt.Sprintf("theme = \"nord\"\n[[vault]]\nname = \"work\"\npath = %q\n[vault.daily]\nformat = \"YYYY-MM-DD\"\n", root)
	require.NoError(t, os.WriteFile(filepath.Join(root, "c.toml"), []byte(src), 0o644))
	c, err := Load(filepath.Join(root, "c.toml"))
	require.NoError(t, err)
	assert.Equal(t, root, c.Root("work"))
	assert.Equal(t, root, c.Root(root))
	assert.Equal(t, "nord", c.Theme)
	assert.Equal(t, DailyCfg{Format: "YYYY-MM-DD"}, c.Daily(root))
	assert.Equal(t, DailyCfg{}, c.Daily("/elsewhere"))
}

func TestAddVaultAppendsAndKeepsComments(t *testing.T) {
	file := filepath.Join(t.TempDir(), "sub", "config.toml")
	require.NoError(t, AddVault(file, "main", "~/git/vault"))
	b, _ := os.ReadFile(file)
	assert.Equal(t, "[[vault]]\nname = \"main\"\npath = \"~/git/vault\"\n", string(b))

	require.NoError(t, os.WriteFile(file, []byte("# my config\ntheme = \"nord\"\n[[vault]]\nname = \"a\"\npath = \"/tmp\""), 0o644))
	require.NoError(t, AddVault(file, "b \"q\"", "/x"))
	c, err := Load(file)
	require.NoError(t, err)
	require.Len(t, c.Vaults, 2)
	assert.Equal(t, "b \"q\"", c.Vaults[1].Name)
	assert.Equal(t, "nord", c.Theme)
	b, _ = os.ReadFile(file)
	assert.Contains(t, string(b), "# my config")
}
