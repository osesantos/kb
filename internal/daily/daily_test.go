package daily

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/osesantos/kb/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.Local) }

func TestFormatsMomentTokens(t *testing.T) {
	cases := []struct {
		format string
		t      time.Time
		want   string
	}{
		{"MMM Do, YYYY", day(2026, 10, 2), "Oct 2nd, 2026"},
		{"MMM Do, YYYY", day(2026, 9, 11), "Sep 11th, 2026"},
		{"MMM Do, YYYY", day(2026, 9, 21), "Sep 21st, 2026"},
		{"MMM Do, YYYY", day(2026, 9, 23), "Sep 23rd, 2026"},
		{"YYYY-MM-DD", day(2026, 1, 5), "2026-01-05"},
		{"YYYY/MMMM/D-M ddd", day(2026, 1, 5), "2026/January/5-1 Mon"},
		{"YYYY-[W]WW dddd", day(2026, 1, 5), "2026-W02 Monday"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, Moment(c.format, c.t), c.format)
	}
}

func TestResolvesConfigOverObsidianOverDefaults(t *testing.T) {
	d := t.TempDir()
	assert.Equal(t, Daily{Folder: "", Format: "YYYY-MM-DD"}, Resolve(d, config.DailyCfg{}))
	require.NoError(t, os.Mkdir(filepath.Join(d, ".obsidian"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(d, ".obsidian/daily-notes.json"), []byte(`{"format":"MMM Do, YYYY","folder":"Journals/"}`), 0o644))
	assert.Equal(t, "Journals/Oct 2nd, 2026.md", Resolve(d, config.DailyCfg{}).Path(day(2026, 10, 2)))
	assert.Equal(t, "Journals/2026-10-02.md", Resolve(d, config.DailyCfg{Format: "YYYY-MM-DD"}).Path(day(2026, 10, 2)))
}
