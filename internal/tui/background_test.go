package tui

import (
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

var sgr = regexp.MustCompile(`^\x1b\[([0-9;:]*)m`)

// unpainted counts visible cells drawn with no background set, as a terminal would track SGR state.
func unpainted(s string) int {
	bad := 0
	for _, line := range strings.Split(s, "\n") {
		bg := false
		for len(line) > 0 {
			if m := sgr.FindStringSubmatch(line); m != nil {
				params := strings.Split(m[1], ";")
				for i := 0; i < len(params); i++ {
					switch params[i] {
					case "", "0", "49":
						bg = false
					case "48":
						bg = true
						if i+1 < len(params) && params[i+1] == "5" {
							i += 2
						} else {
							i += 4
						}
					case "38":
						if i+1 < len(params) && params[i+1] == "5" {
							i += 2
						} else {
							i += 4
						}
					}
				}
				line = line[len(m[0]):]
				continue
			}
			_, size := utf8.DecodeRuneInString(line)
			if !bg {
				bad++
			}
			line = line[size:]
		}
	}
	return bad
}

func TestPopupsPaintEveryCellOnTheModalBackground(t *testing.T) {
	_, m := newApp(t)
	press(m, "s")
	typeStr(m, "nowhere")
	assert.Zero(t, unpainted(m.finderContent(80, 20)), "search popup")
	press(m, "esc", "enter", "tab")
	content, _ := m.pickerContent()
	assert.Zero(t, unpainted(content), "link picker")
	press(m, "esc", "esc")
	command(m, "vaults")
	press(m, "n")
	content, _ = m.formContent()
	assert.Zero(t, unpainted(content), "new-vault form")
	assert.Positive(t, unpainted("\x1b[31mred\x1b[m plain"), "the checker itself sees unpainted text")
}
