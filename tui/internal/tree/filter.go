package tree

import (
	"strings"

	"github.com/afruth/wtree/tui/internal/treetable"
)

type termKind int

const (
	termText termKind = iota
	termIs
	termState
	termRepo
	termBranch
)

type term struct {
	kind  termKind
	neg   bool
	value string // lower case
}

// Filter is a parsed search. See ParseFilter.
type Filter struct {
	raw     string
	terms   []term
	invalid []string
}

var isValues = map[string]bool{"current": true, "main": true, "orphan": true, "merged": true, "stale": true, "changed": true, "locked": true, "marked": true, "open": true}
var stateValues = map[string]bool{"running": true, "partial": true, "exited": true, "down": true, "volumes": true, "none": true}

// ParseFilter reads the search syntax of ux.md 4.2: substring and case-insensitive,
// terms separated by spaces and ANDed, -term negates, quotes group, and the fields
// is:, state:, repo: and branch:.
//
// A quoted term is literal text, also with a colon in it. An unquoted term with a
// colon is a field, and an unknown field, an unknown value or an empty value makes the
// whole filter invalid. An invalid filter matches nothing, so a typo cannot widen a
// mark-all. That holds for a negated term too, and for the later fields disk:, age:
// and sort:, which v1 does not know.
func ParseFilter(query string) Filter {
	f := Filter{raw: query}
	for _, tok := range tokenize(query) {
		neg := tok.neg
		if tok.quoted {
			if tok.value != "" {
				f.terms = append(f.terms, term{termText, neg, strings.ToLower(tok.value)})
			}
			continue
		}
		field, value, hasField := strings.Cut(tok.value, ":")
		if !hasField {
			if tok.value != "" {
				f.terms = append(f.terms, term{termText, neg, strings.ToLower(tok.value)})
			} else if neg {
				f.invalid = append(f.invalid, "-")
			}
			continue
		}
		value = strings.ToLower(value + tok.rest)
		switch strings.ToLower(field) {
		case "is":
			if !isValues[value] {
				f.invalid = append(f.invalid, "is:"+value)
				continue
			}
			f.terms = append(f.terms, term{termIs, neg, value})
		case "state":
			if !stateValues[value] {
				f.invalid = append(f.invalid, "state:"+value)
				continue
			}
			f.terms = append(f.terms, term{termState, neg, value})
		case "repo", "branch":
			if value == "" {
				f.invalid = append(f.invalid, strings.ToLower(field)+":")
				continue
			}
			kind := termRepo
			if strings.EqualFold(field, "branch") {
				kind = termBranch
			}
			f.terms = append(f.terms, term{kind, neg, value})
		default:
			f.invalid = append(f.invalid, field+":")
		}
	}
	return f
}

type token struct {
	value  string
	rest   string // a quoted value after a field: repo:"my repo"
	neg    bool
	quoted bool
}

func tokenize(q string) []token {
	var out []token
	rs := []rune(q)
	for i := 0; i < len(rs); {
		if rs[i] == ' ' || rs[i] == '\t' {
			i++
			continue
		}
		var t token
		if rs[i] == '-' {
			t.neg = true
			i++
		}
		var b strings.Builder
		for i < len(rs) && rs[i] != ' ' && rs[i] != '\t' {
			if rs[i] == '"' {
				j := i + 1
				for j < len(rs) && rs[j] != '"' {
					j++
				}
				inner := string(rs[i+1 : min(j, len(rs))])
				if b.Len() == 0 {
					t.quoted = true
					b.WriteString(inner)
				} else {
					t.rest += inner
				}
				i = min(j+1, len(rs))
				continue
			}
			if t.quoted {
				b.WriteRune(rs[i])
			} else if t.rest != "" {
				t.rest += string(rs[i])
			} else {
				b.WriteRune(rs[i])
			}
			i++
		}
		t.value = b.String()
		out = append(out, t)
	}
	return out
}

// Active says that the filter does something.
func (f Filter) Active() bool { return len(f.terms) > 0 || len(f.invalid) > 0 }

// Invalid says that the filter matches nothing because of a term it does not know.
func (f Filter) Invalid() bool { return len(f.invalid) > 0 }

// Error is the status line for an invalid filter: "unknown filter: foo:", or "".
func (f Filter) Error() string {
	if len(f.invalid) == 0 {
		return ""
	}
	return "unknown filter: " + treetable.Sanitize(strings.Join(f.invalid, " "))
}

// String is the text that the filter was parsed from.
func (f Filter) String() string { return f.raw }

// facts is what a row offers to a filter. The text and the repo and branch come with
// what the row inherits from its worktree, as ux.md 4.2 asks: a stack matches on the
// branch of its worktree. The is: and state: facts belong to the row itself, so
// state:exited does not put a worktree in a mark-all with its stacks.
type facts struct {
	text   string // lower case
	repo   string
	branch string
	is     map[string]bool
	state  string
}

func (f Filter) match(x *facts) bool {
	if len(f.invalid) > 0 {
		return false
	}
	for _, t := range f.terms {
		var ok bool
		switch t.kind {
		case termText:
			ok = strings.Contains(x.text, t.value)
		case termIs:
			ok = x.is[t.value]
		case termState:
			ok = x.state == t.value
		case termRepo:
			ok = strings.Contains(x.repo, t.value)
		case termBranch:
			ok = strings.Contains(x.branch, t.value)
		}
		if ok == t.neg {
			return false
		}
	}
	return true
}
