package discover

import (
	"errors"
	"fmt"
	"strings"
)

// QuarkFile holds the fields we read from a .quark file.
type QuarkFile struct {
	Name, Summary, Version string
	Dependencies           []string
}

// ParseQuarkFile reads the Event literal `( key: value, ... )` of a .quark
// file. It only looks at text; it never evaluates sclang. Missing keys stay
// empty; text without a closed "( ... )" or with an unterminated string is an
// error.
func ParseQuarkFile(src string) (QuarkFile, error) {
	open := strings.Index(src, "(")
	if open < 0 {
		return QuarkFile{}, errors.New("parse .quark file: no ( ... ) event found")
	}
	p := &quarkParser{src: src, pos: open + 1}
	q, err := p.event()
	if err != nil {
		return QuarkFile{}, fmt.Errorf("parse .quark file: %w", err)
	}
	return q, nil
}

// quarkParser walks the event literal one token at a time.
type quarkParser struct {
	src string
	pos int
}

// event reads key: value pairs up to the closing parenthesis.
func (p *quarkParser) event() (QuarkFile, error) {
	var q QuarkFile
	for {
		p.skipTrivia()
		if p.eof() {
			return q, errors.New("missing closing )")
		}
		switch c := p.src[p.pos]; {
		case c == ')':
			return q, nil
		case c == ',':
			p.pos++
		default:
			if err := p.pair(&q); err != nil {
				return q, err
			}
		}
	}
}

// pair reads one "key: value" and stores the value if we know the key.
func (p *quarkParser) pair(q *QuarkFile) error {
	key := p.word()
	p.skipTrivia()
	if key == "" || p.eof() || p.src[p.pos] != ':' {
		return p.skipValue() // not a key: value pair; skip it
	}
	p.pos++
	p.skipTrivia()
	switch key {
	case "name":
		return p.stringInto(&q.Name)
	case "summary":
		return p.stringInto(&q.Summary)
	case "version":
		return p.stringInto(&q.Version)
	case "dependencies":
		return p.listInto(&q.Dependencies)
	}
	return p.skipValue()
}

// stringInto stores a string value; other value kinds are skipped.
func (p *quarkParser) stringInto(dst *string) error {
	if p.eof() || p.src[p.pos] != '"' {
		return p.skipValue()
	}
	s, err := p.quoted('"')
	*dst = s
	return err
}

// listInto reads [ "a", \B, 'C' ] into dst; other value kinds are skipped.
func (p *quarkParser) listInto(dst *[]string) error {
	if p.eof() || p.src[p.pos] != '[' {
		return p.skipValue()
	}
	p.pos++
	for {
		p.skipTrivia()
		if p.eof() {
			return errors.New("missing closing ]")
		}
		switch c := p.src[p.pos]; {
		case c == ']':
			p.pos++
			return nil
		case c == ',':
			p.pos++
		case c == ')':
			return errors.New("missing closing ]")
		case c == '"' || c == '\'':
			s, err := p.quoted(c)
			if err != nil {
				return err
			}
			*dst = append(*dst, s)
		case c == '\\':
			p.pos++
			*dst = append(*dst, p.word())
		default:
			if err := p.skipValue(); err != nil {
				return err
			}
		}
	}
}

// skipValue skips to the next top-level "," or ")" without consuming it. A
// stray closing bracket is consumed so the caller always makes progress.
func (p *quarkParser) skipValue() error {
	start := p.pos
	if err := p.scanValue(); err != nil {
		return err
	}
	if p.pos == start && !p.eof() && strings.IndexByte(",)", p.src[p.pos]) < 0 {
		p.pos++
	}
	return nil
}

func (p *quarkParser) scanValue() error {
	depth := 0
	for !p.eof() {
		switch c := p.src[p.pos]; {
		case c == '"' || c == '\'':
			if _, err := p.quoted(c); err != nil {
				return err
			}
			continue
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			if depth == 0 {
				return nil
			}
			depth--
		case c == ',' && depth == 0:
			return nil
		}
		p.pos++
	}
	return nil
}

// quoted reads a string or symbol delimited by quote, resolving escapes.
func (p *quarkParser) quoted(quote byte) (string, error) {
	var b strings.Builder
	for p.pos++; !p.eof(); p.pos++ {
		switch c := p.src[p.pos]; c {
		case quote:
			p.pos++
			return b.String(), nil
		case '\\':
			p.pos++
			if p.eof() {
				return "", errors.New("unterminated string")
			}
			b.WriteByte(unescape(p.src[p.pos]))
		default:
			b.WriteByte(c)
		}
	}
	return "", errors.New("unterminated string")
}

func unescape(c byte) byte {
	switch c {
	case 'n':
		return '\n'
	case 't':
		return '\t'
	}
	return c
}

// word reads an identifier made of letters, digits and underscores.
func (p *quarkParser) word() string {
	start := p.pos
	for !p.eof() && isWordChar(p.src[p.pos]) {
		p.pos++
	}
	return p.src[start:p.pos]
}

func isWordChar(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// skipTrivia skips whitespace and // and /* */ comments.
func (p *quarkParser) skipTrivia() {
	for !p.eof() {
		switch {
		case strings.IndexByte(" \t\r\n", p.src[p.pos]) >= 0:
			p.pos++
		case strings.HasPrefix(p.src[p.pos:], "//"):
			p.skipTo("\n")
		case strings.HasPrefix(p.src[p.pos:], "/*"):
			p.skipTo("*/")
		default:
			return
		}
	}
}

// skipTo moves past the next occurrence of end, or to the end of the text.
func (p *quarkParser) skipTo(end string) {
	i := strings.Index(p.src[p.pos+2:], end)
	if i < 0 {
		p.pos = len(p.src)
		return
	}
	p.pos += 2 + i + len(end)
}

func (p *quarkParser) eof() bool { return p.pos >= len(p.src) }
