package styles

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/lucasb-eyer/go-colorful"
	"github.com/stretchr/testify/assert"
)

func omarchyFixture(t *testing.T, colors string) string {
	dir := t.TempDir()
	assert.NoError(t, os.MkdirAll(filepath.Join(dir, "theme"), 0o755))
	assert.NoError(t, os.WriteFile(filepath.Join(dir, "theme", "colors.toml"), []byte(colors), 0o644))
	return dir
}

func hex(t *testing.T, c color.Color) string {
	v, ok := colorful.MakeColor(c)
	assert.True(t, ok)
	return v.Hex()
}

func TestOmarchyThemeMapsThePalette(t *testing.T) {
	dir := omarchyFixture(t, `mode = "dark"
accent = "#f38d70"
background = "#2c2525"
foreground = "#e6d9db"
red = "#fd6883"
`)
	th := ResolveTheme("", dir)
	assert.Equal(t, "#f38d70", hex(t, th.Primary))
	assert.Equal(t, "#e6d9db", hex(t, th.Text))
	assert.Equal(t, "#fd6883", hex(t, th.Danger))
	assert.Equal(t, "#f38d70", hex(t, th.Warning), "a missing colour falls back to accent")
	assert.Equal(t, hex(t, th.Primary), hex(t, ResolveTheme("omarchy", dir).Primary))
}

func TestResolveThemeFallsBack(t *testing.T) {
	dark := hex(t, DarkTheme().Primary)
	assert.Equal(t, dark, hex(t, ResolveTheme("", t.TempDir()).Primary), "no Omarchy")
	assert.Equal(t, dark, hex(t, ResolveTheme("", omarchyFixture(t, "accent = [")).Primary), "malformed")
	assert.Equal(t, dark, hex(t, ResolveTheme("", omarchyFixture(t, `accent = "#ffffff"`)).Primary), "incomplete")
	assert.Equal(t, dark, hex(t, ResolveTheme("", "").Primary))
}

func TestNamedThemeBeatsOmarchy(t *testing.T) {
	dir := omarchyFixture(t, `accent = "#f38d70"
background = "#2c2525"
foreground = "#e6d9db"`)
	assert.Equal(t, hex(t, NordTheme().Primary), hex(t, ResolveTheme("nord", dir).Primary))
}
