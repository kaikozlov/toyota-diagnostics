package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"
)

// decodeStoredJSON decodes the first JSON value in raw exactly as
// encoding/json does into an interface with UseNumber: numbers stay
// json.Number, a repeated name keeps its last value, and empty arrays are
// non-nil. It builds the tree directly rather than through reflection.
func decodeStoredJSON(raw []byte) (any, error) {
	p := jsonParser{data: raw}
	return p.value(true)
}

// storedMembers decodes the named members of a packed JSON object. The others
// are checked as JSON but not built.
func storedMembers(packed []byte, keys []string) (map[string]any, error) {
	raw, err := inflateDocument(packed)
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]bool, len(keys))
	for _, key := range keys {
		wanted[key] = true
	}
	p := jsonParser{data: raw}
	if p.skipSpace(); p.pos >= len(p.data) || p.data[p.pos] != '{' {
		if _, err := p.value(false); err != nil {
			return nil, err
		}
		return nil, errors.New("stored value is not an object")
	}
	return p.object(true, wanted)
}

// maxJSONDepth matches encoding/json's nesting limit.
const maxJSONDepth = 10000

type jsonParser struct {
	data  []byte
	pos   int
	depth int
}

func (p *jsonParser) skipSpace() {
	for p.pos < len(p.data) {
		switch p.data[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *jsonParser) fail() error {
	if p.pos >= len(p.data) {
		return errors.New("unexpected end of JSON input")
	}
	return fmt.Errorf("invalid character %q at offset %d", p.data[p.pos], p.pos)
}

// value reads one JSON value, building it only when build is set.
func (p *jsonParser) value(build bool) (any, error) {
	p.skipSpace()
	if p.pos >= len(p.data) {
		return nil, p.fail()
	}
	switch c := p.data[p.pos]; {
	case c == '{':
		object, err := p.object(build, nil)
		if err != nil || !build {
			return nil, err
		}
		return object, nil
	case c == '[':
		return p.array(build)
	case c == '"':
		text, err := p.text(build)
		if err != nil || !build {
			return nil, err
		}
		return text, nil
	case c == '-' || c >= '0' && c <= '9':
		return p.number(build)
	case c == 't':
		return true, p.literal("true")
	case c == 'f':
		return false, p.literal("false")
	case c == 'n':
		return nil, p.literal("null")
	}
	return nil, p.fail()
}

func (p *jsonParser) nest() error {
	if p.depth++; p.depth > maxJSONDepth {
		return errors.New("exceeded max depth")
	}
	return nil
}

// object reads an object when build is set, keeping only wanted members
// unless wanted is nil.
func (p *jsonParser) object(build bool, wanted map[string]bool) (map[string]any, error) {
	if err := p.nest(); err != nil {
		return nil, err
	}
	var object map[string]any
	if build {
		object = map[string]any{}
	}
	p.pos++
	p.skipSpace()
	if p.pos < len(p.data) && p.data[p.pos] == '}' {
		p.pos++
		p.depth--
		return object, nil
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.data) || p.data[p.pos] != '"' {
			return nil, p.fail()
		}
		name, err := p.text(build)
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if p.pos >= len(p.data) || p.data[p.pos] != ':' {
			return nil, p.fail()
		}
		p.pos++
		keep := build && (wanted == nil || wanted[name])
		value, err := p.value(keep)
		if err != nil {
			return nil, err
		}
		if keep {
			object[name] = value
		}
		p.skipSpace()
		if p.pos >= len(p.data) {
			return nil, p.fail()
		}
		switch p.data[p.pos] {
		case ',':
			p.pos++
		case '}':
			p.pos++
			p.depth--
			return object, nil
		default:
			return nil, p.fail()
		}
	}
}

func (p *jsonParser) array(build bool) (any, error) {
	if err := p.nest(); err != nil {
		return nil, err
	}
	p.pos++
	var array []any
	if build {
		array = []any{}
	}
	p.skipSpace()
	if p.pos < len(p.data) && p.data[p.pos] == ']' {
		p.pos++
		p.depth--
		return array, nil
	}
	for {
		value, err := p.value(build)
		if err != nil {
			return nil, err
		}
		if build {
			array = append(array, value)
		}
		p.skipSpace()
		if p.pos >= len(p.data) {
			return nil, p.fail()
		}
		switch p.data[p.pos] {
		case ',':
			p.pos++
		case ']':
			p.pos++
			p.depth--
			return array, nil
		default:
			return nil, p.fail()
		}
	}
}

// text reads a string literal, building it only when build is set. Plain ASCII and valid UTF-8 without escapes
// convert directly; anything else is unquoted by encoding/json itself, so
// escapes and invalid UTF-8 decode exactly as they always have.
func (p *jsonParser) text(build bool) (string, error) {
	start := p.pos
	p.pos++
	plain := true
	for p.pos < len(p.data) {
		switch c := p.data[p.pos]; {
		case c == '"':
			p.pos++
			literal := p.data[start:p.pos]
			if plain {
				if !build {
					return "", nil
				}
				return string(literal[1 : len(literal)-1]), nil
			}
			var s string
			if err := json.Unmarshal(literal, &s); err != nil {
				return "", err
			}
			return s, nil
		case c == '\\':
			plain = false
			p.pos += 2
		case c < 0x20:
			return "", p.fail()
		case c >= utf8.RuneSelf:
			r, size := utf8.DecodeRune(p.data[p.pos:])
			if r == utf8.RuneError && size == 1 {
				plain = false
			}
			p.pos += size
		default:
			p.pos++
		}
	}
	p.pos = len(p.data)
	return "", p.fail()
}

// number checks the JSON number grammar and keeps the literal text.
func (p *jsonParser) number(build bool) (any, error) {
	start := p.pos
	digits := func() bool {
		begin := p.pos
		for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
			p.pos++
		}
		return p.pos > begin
	}
	if p.data[p.pos] == '-' {
		p.pos++
	}
	if p.pos < len(p.data) && p.data[p.pos] == '0' {
		p.pos++
	} else if !digits() {
		return nil, p.fail()
	}
	if p.pos < len(p.data) && p.data[p.pos] == '.' {
		p.pos++
		if !digits() {
			return nil, p.fail()
		}
	}
	if p.pos < len(p.data) && (p.data[p.pos] == 'e' || p.data[p.pos] == 'E') {
		p.pos++
		if p.pos < len(p.data) && (p.data[p.pos] == '+' || p.data[p.pos] == '-') {
			p.pos++
		}
		if !digits() {
			return nil, p.fail()
		}
	}
	if !build {
		return nil, nil
	}
	return json.Number(p.data[start:p.pos]), nil
}

func (p *jsonParser) literal(word string) error {
	if len(p.data)-p.pos < len(word) || string(p.data[p.pos:p.pos+len(word)]) != word {
		return p.fail()
	}
	p.pos += len(word)
	return nil
}
