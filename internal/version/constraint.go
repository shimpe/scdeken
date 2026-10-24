package version

import (
	"fmt"
	"strings"
)

// term is one comparison such as ">=1.2".
type term struct {
	op string // "=", ">=", "<=", ">" or "<"
	v  Version
}

// Constraint is a set of comparisons that must all hold. The zero value
// allows every version.
type Constraint struct {
	terms []term
}

// operators lists the comparison prefixes, longest first so ">=" is not read as ">".
var operators = []string{">=", "<=", ">", "<", "="}

// ParseConstraint reads a version constraint: "*" or "" (any version), an
// exact version "1.2" or "=1.2", a comparison ">=1.0", "<=1.0", ">1.0" or
// "<1.0", or several of these joined with commas, which must all hold
// (">=1.0,<2.0").
func ParseConstraint(s string) (Constraint, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "*" {
		return Constraint{}, nil
	}
	var c Constraint
	for _, part := range strings.Split(s, ",") {
		t, err := parseTerm(strings.TrimSpace(part))
		if err != nil {
			return Constraint{}, fmt.Errorf("version constraint %q: %w", s, err)
		}
		c.terms = append(c.terms, t)
	}
	return c, nil
}

func parseTerm(s string) (term, error) {
	op := "="
	for _, candidate := range operators {
		if rest, ok := strings.CutPrefix(s, candidate); ok {
			op, s = candidate, strings.TrimSpace(rest)
			break
		}
	}
	v, err := Parse(s)
	if err != nil {
		return term{}, err
	}
	return term{op: op, v: v}, nil
}

// Allows reports whether v satisfies every comparison. A version with build
// metadata (an untagged commit) satisfies only the constraint that allows
// everything, because its place in the version order is unknown.
func (c Constraint) Allows(v Version) bool {
	if len(c.terms) == 0 {
		return true
	}
	if v.IsUnstable() {
		return false
	}
	for _, t := range c.terms {
		if !t.holds(v) {
			return false
		}
	}
	return true
}

func (t term) holds(v Version) bool {
	cmp := v.Compare(t.v)
	switch t.op {
	case ">=":
		return cmp >= 0
	case "<=":
		return cmp <= 0
	case ">":
		return cmp > 0
	case "<":
		return cmp < 0
	}
	return cmp == 0
}

// String writes the constraint in canonical form: "*" for any version, else
// the comparisons joined with commas.
func (c Constraint) String() string {
	if len(c.terms) == 0 {
		return "*"
	}
	parts := make([]string, len(c.terms))
	for i, t := range c.terms {
		parts[i] = t.op + t.v.String()
	}
	return strings.Join(parts, ",")
}
