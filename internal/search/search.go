// Package search ranks heading sections of the in-memory vault with BM25, so agents read a section, not a file.
package search

import (
	"cmp"
	"encoding/json"
	"math"
	"runtime"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/osesantos/kb/internal/vault"
)

const (
	k1      = 1.2
	b       = 0.75
	perNote = 3
)

// Hit is one ranked section of a note.
type Hit struct {
	Note, Section int
	Score         float64
	// Line is the 1-based line of the first section line matching the query, 0 when none; Snippet is its text.
	Line    int
	Snippet string
}

// count returns occurrences of w starting at a word boundary, so `commit` also matches `commits` but not `recommit`.
func count(hay, w string) int {
	n := 0
	for off := 0; ; {
		i := strings.Index(hay[off:], w)
		if i < 0 {
			return n
		}
		i += off
		prev, _ := utf8.DecodeLastRuneInString(hay[:i])
		if i == 0 || !(unicode.IsLetter(prev) || unicode.IsNumber(prev)) {
			n++
		}
		off = i + len(w)
	}
}

// termFreqs counts every query word per section of every note, one chunk of notes per CPU.
func termFreqs(v *vault.Vault, words []string) [][][]int {
	tfs := make([][][]int, len(v.Notes))
	workers := runtime.NumCPU()
	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			for i := w; i < len(v.Notes); i += workers {
				n := &v.Notes[i]
				tfs[i] = make([][]int, len(n.Sections))
				for si, s := range n.Sections {
					lower := s.Lower(n)
					tf := make([]int, len(words))
					for k, word := range words {
						tf[k] = count(lower, word)
					}
					tfs[i][si] = tf
				}
			}
		})
	}
	wg.Wait()
	return tfs
}

func firstLine(n *vault.Note, s vault.Section, phrase, first string) (int, string) {
	lines := strings.Split(s.Lower(n), "\n")
	i := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, phrase) })
	if i < 0 {
		i = slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, first) })
	}
	orig := strings.Split(s.Text(n), "\n")
	if i < 0 || i >= len(orig) {
		return 0, ""
	}
	text := strings.TrimSpace(orig[i])
	if r := []rune(text); len(r) > 160 {
		text = string(r[:160])
	}
	return s.Start + i, text
}

// Lexical returns BM25-ranked sections of notes that contain every query word (in path, title or body), at most three per note.
func Lexical(v *vault.Vault, query string, limit int) []Hit {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return nil
	}
	tfs := termFreqs(v, words)

	total, length := 0, 0
	df := make([]int, len(words))
	for i, n := range v.Notes {
		for si, s := range n.Sections {
			total++
			length += s.LowerLen()
			for k, f := range tfs[i][si] {
				if f > 0 {
					df[k]++
				}
			}
		}
	}
	total = max(total, 1)
	avg := float64(length) / float64(total)
	idf := make([]float64, len(words))
	for k := range words {
		idf[k] = math.Log(1 + (float64(total)-float64(df[k])+0.5)/(float64(df[k])+0.5))
	}
	phrase := strings.Join(words, " ")

	hits := []Hit{}
	for i := range v.Notes {
		n := &v.Notes[i]
		path := strings.ToLower(n.Path)
		matches := true
		for k, w := range words {
			if !strings.Contains(path, w) && !slices.ContainsFunc(tfs[i], func(t []int) bool { return t[k] > 0 }) {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		title := strings.ToLower(n.Title)
		boost := 1 + 0.1*math.Log(1+float64(len(v.Backlinks[i])))
		titles := 0.0
		for k, w := range words {
			if strings.Contains(title, w) {
				titles += 2 * idf[k]
			}
		}
		hit := func(si int, score float64) Hit {
			line, snip := firstLine(n, n.Sections[si], phrase, words[0])
			return Hit{Note: i, Section: si, Score: (score + titles) * boost, Line: line, Snippet: snip}
		}
		secs := []Hit{}
		for si, s := range n.Sections {
			head := strings.ToLower(s.Heading)
			norm := k1 * (1 - b + b*float64(s.LowerLen())/avg)
			bm, heads := 0.0, 0.0
			for k, f := range tfs[i][si] {
				bm += idf[k] * float64(f) * (k1 + 1) / (float64(f) + norm)
				if strings.Contains(head, words[k]) {
					heads += idf[k]
				}
			}
			if bm+heads > 0 {
				secs = append(secs, hit(si, bm+heads))
			}
		}
		if len(secs) == 0 {
			secs = append(secs, hit(0, 0))
		}
		slices.SortStableFunc(secs, func(a, b Hit) int { return cmp.Compare(b.Score, a.Score) })
		hits = append(hits, secs[:min(len(secs), perNote)]...)
	}
	// Notes are sorted by path, so note index order is path order.
	slices.SortFunc(hits, func(a, c Hit) int {
		return cmp.Or(cmp.Compare(c.Score, a.Score), cmp.Compare(a.Note, c.Note), cmp.Compare(a.Section, c.Section))
	})
	return hits[:min(len(hits), limit)]
}

// JSON renders hits for agents; with a token budget, each hit carries its section text until the budget is spent.
func JSON(v *vault.Vault, hits []Hit, budget *int) string {
	left := 0
	if budget != nil {
		left = *budget
	}
	out := make([]map[string]any, 0, len(hits))
	for _, h := range hits {
		n := &v.Notes[h.Note]
		s := n.Sections[h.Section]
		j := map[string]any{
			"path": n.Path, "title": n.Title, "heading": s.Heading, "score": math.Round(h.Score*100) / 100, "provider": "lexical",
			"line": nil, "snippet": nil, "line_start": s.Start, "line_end": s.End, "tokens": s.Tokens(),
		}
		if h.Line > 0 {
			j["line"], j["snippet"] = h.Line, h.Snippet
		}
		if budget != nil && s.Tokens() <= left {
			left -= s.Tokens()
			j["text"] = s.Text(n)
		}
		out = append(out, j)
	}
	b, _ := json.Marshal(out)
	return string(b)
}
