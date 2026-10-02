package vault

import "strings"

// Section is a `##`/`###` block of a note (the `#` title, frontmatter and intro form the first); long sections are split at blank lines.
type Section struct {
	Heading string
	// Start and End are the 1-based inclusive line range in the note.
	Start, End int
	body       [2]int
	lower      [2]int
}

// Text is the section's slice of the note body.
func (s Section) Text(n *Note) string { return n.Body[s.body[0]:s.body[1]] }

// Lower is the section's slice of the lowercased note body.
func (s Section) Lower(n *Note) string { return n.Lower[s.lower[0]:s.lower[1]] }

// LowerLen is the section's length in bytes of lowercased text, the BM25 document length.
func (s Section) LowerLen() int { return s.lower[1] - s.lower[0] }

// Tokens is a rough token count (4 bytes per token), enough to budget agent reads.
func (s Section) Tokens() int { return (s.body[1] - s.body[0] + 3) / 4 }

const maxSection = 6 * 1024

// heading parses an ATX heading line into its level and text.
func heading(l string) (int, string, bool) {
	n := len(l) - len(strings.TrimLeft(l, "#"))
	if n < 1 || n > 6 || !strings.HasPrefix(l[n:], " ") {
		return 0, "", false
	}
	return n, strings.TrimSpace(l[n:]), true
}

type line struct {
	text  string
	level int
	head  string
}

// outline splits body into lines (newline kept) with their heading level; `#` inside code fences is not a heading.
func outline(body string) []line {
	out := []line{}
	fence := false
	for _, l := range strings.SplitAfter(body, "\n") {
		if l == "" {
			continue
		}
		t := strings.TrimLeft(l, " \t")
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fence = !fence
			out = append(out, line{text: l})
			continue
		}
		ln := line{text: l}
		if !fence {
			ln.level, ln.head, _ = heading(l)
		}
		out = append(out, ln)
	}
	return out
}

// Sections splits a note into sections; lower is body lowercased, which has the same lines.
func Sections(body, lower string) []Section {
	out := []Section{}
	lowers := strings.SplitAfter(lower, "\n")
	b, lo := 0, 0
	for i, l := range outline(body) {
		ll := ""
		if i < len(lowers) {
			ll = lowers[i]
		}
		isHead := l.level == 2 || l.level == 3
		split := len(out) == 0 || isHead || (out[len(out)-1].body[1]-out[len(out)-1].body[0] > maxSection && strings.TrimSpace(l.text) == "")
		if split {
			h := ""
			switch {
			case isHead:
				h = l.head
			case len(out) > 0:
				h = out[len(out)-1].Heading
			}
			out = append(out, Section{Heading: h, Start: i + 1, End: i + 1, body: [2]int{b, b}, lower: [2]int{lo, lo}})
		}
		s := &out[len(out)-1]
		s.End = i + 1
		s.body[1] = b + len(l.text)
		s.lower[1] = lo + len(ll)
		b += len(l.text)
		lo += len(ll)
	}
	if len(out) == 0 {
		out = append(out, Section{Start: 1, End: 1})
	}
	return out
}

// Block is the text under the heading named want (case-insensitive), down to the next heading of the same or higher level.
func Block(body, want string) (string, bool) {
	want = strings.ToLower(strings.TrimSpace(want))
	off, start, level := 0, -1, 0
	for _, l := range outline(body) {
		switch {
		case l.level > 0 && start >= 0 && l.level <= level:
			return body[start:off], true
		case l.level > 0 && start < 0 && strings.ToLower(l.head) == want:
			start, level = off, l.level
		}
		off += len(l.text)
	}
	if start < 0 {
		return "", false
	}
	return body[start:], true
}
