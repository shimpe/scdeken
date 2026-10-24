// Package scclass extracts class and UGen names from sclang source.
//
// It is a small scanner, not a parser: it only needs to find class headers
// at brace depth 0 while ignoring comments, strings, symbols and characters.
package scclass

import (
	"sort"
	"strings"
)

// Class is one class definition: its name and its superclass ("" if none).
type Class struct{ Name, Super string }

// UGenBases lists the sclang classes whose subclasses are UGens.
var UGenBases = []string{"UGen", "MultiOutUGen", "PureUGen", "PureMultiOutUGen", "Filter", "Panner", "XLine", "WidthFirstUGen", "BufInfoUGenBase", "InfoUGenBase", "AbstractIn", "AbstractOut", "LagControl"}

// Parse returns the classes defined in src, in order. Only top-level
// `Name : Super {` and `Name {` headers count; `+ Name {` extensions and
// metaclass names (`Meta_...`) are skipped. Malformed input never panics:
// whatever was found before the problem is returned.
func Parse(src []byte) []Class {
	s := &scanner{src: src}
	var classes []Class
	depth := 0
	for {
		s.skipTrivia()
		if s.done() {
			return classes
		}
		switch c := s.src[s.pos]; {
		case c == '{':
			depth++
			s.advance(1)
		case c == '}':
			if depth > 0 {
				depth--
			}
			s.advance(1)
		case c == '"' || c == '\'':
			s.skipQuoted(c)
		case c == '$':
			s.skipChar()
		case c == '+' && depth == 0:
			s.advance(1)
			s.skipTrivia()
			s.ident() // the extended class is not a definition
		case isIdentChar(c):
			if depth == 0 && isUpper(c) {
				if cl, ok := s.header(); ok && !strings.HasPrefix(cl.Name, "Meta_") {
					classes = append(classes, cl)
				}
			} else {
				s.ident()
			}
		default:
			s.advance(1)
		}
	}
}

// Split divides classes into UGen names and all other names, each sorted and
// de-duplicated. A class is a UGen if its superclass is in UGenBases or is
// itself a UGen class among classes.
func Split(classes []Class) (ugens, others []string) {
	isUGen := map[string]bool{}
	for _, b := range UGenBases {
		isUGen[b] = true
	}
	ugenClass := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, c := range classes {
			if isUGen[c.Super] && !ugenClass[c.Name] {
				ugenClass[c.Name], isUGen[c.Name], changed = true, true, true
			}
		}
	}
	other := map[string]bool{}
	for _, c := range classes {
		if !ugenClass[c.Name] {
			other[c.Name] = true
		}
	}
	return sortedKeys(ugenClass), sortedKeys(other)
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

type scanner struct {
	src []byte
	pos int
}

func (s *scanner) done() bool { return s.pos >= len(s.src) }

// peek returns the current byte, or 0 at the end of input.
func (s *scanner) peek() byte {
	if s.done() {
		return 0
	}
	return s.src[s.pos]
}

func (s *scanner) hasPrefix(p string) bool {
	return strings.HasPrefix(string(s.src[s.pos:min(len(s.src), s.pos+len(p))]), p)
}

// header reads `Name [slot] [: Super]` and reports whether a `{` follows.
// It leaves the `{` unread. On failure the position is just after Name.
func (s *scanner) header() (Class, bool) {
	name := s.ident()
	afterName := s.pos
	s.skipTrivia()
	if s.peek() == '[' {
		s.skipUntil(']')
		s.skipTrivia()
	}
	super := ""
	if s.peek() == ':' {
		s.advance(1)
		s.skipTrivia()
		if super = s.ident(); super == "" {
			s.pos = afterName
			return Class{}, false
		}
		s.skipTrivia()
	}
	if s.peek() != '{' {
		s.pos = afterName
		return Class{}, false
	}
	return Class{name, super}, true
}

// skipTrivia skips whitespace and comments.
func (s *scanner) skipTrivia() {
	for !s.done() {
		switch {
		case isSpace(s.src[s.pos]):
			s.advance(1)
		case s.hasPrefix("//"):
			s.skipUntil('\n')
		case s.hasPrefix("/*"):
			s.skipBlockComment()
		default:
			return
		}
	}
}

// skipBlockComment skips a /* */ comment; sclang comments nest.
func (s *scanner) skipBlockComment() {
	depth := 0
	for !s.done() {
		switch {
		case s.hasPrefix("/*"):
			depth++
			s.advance(2)
		case s.hasPrefix("*/"):
			depth--
			s.advance(2)
			if depth == 0 {
				return
			}
		default:
			s.advance(1)
		}
	}
}

// skipQuoted skips a "string" or 'symbol' whose opening quote is current.
func (s *scanner) skipQuoted(quote byte) {
	s.advance(1)
	for !s.done() {
		switch s.src[s.pos] {
		case '\\':
			s.advance(2)
		case quote:
			s.advance(1)
			return
		default:
			s.advance(1)
		}
	}
}

// skipChar skips a character literal: $x or $\x.
func (s *scanner) skipChar() {
	s.advance(1)
	if s.peek() == '\\' {
		s.advance(1)
	}
	s.advance(1)
}

// skipUntil moves past the next occurrence of b, or to the end of input.
func (s *scanner) skipUntil(b byte) {
	for !s.done() && s.src[s.pos] != b {
		s.advance(1)
	}
	s.advance(1)
}

// ident reads an identifier ([A-Za-z0-9_]+) and returns it.
func (s *scanner) ident() string {
	start := s.pos
	for !s.done() && isIdentChar(s.src[s.pos]) {
		s.advance(1)
	}
	return string(s.src[start:s.pos])
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }
func isUpper(c byte) bool { return c >= 'A' && c <= 'Z' }
func isIdentChar(c byte) bool {
	return isUpper(c) || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_'
}

// advance moves forward n bytes, never past the end of input.
func (s *scanner) advance(n int) { s.pos = min(s.pos+n, len(s.src)) }
