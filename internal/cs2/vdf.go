package cs2

import "fmt"

// Минимальный разбор VDF (KeyValues) — формата, в котором Steam хранит
// libraryfolders.vdf, а игра читает gamestate_integration_*.cfg.
//
// Своя реализация вместо зависимости: нужен один вложенный словарь строк, а
// у проекта сейчас четыре прямые зависимости, и это его достоинство.

// vdfMap — узел дерева: значения либо string, либо vdfMap.
type vdfMap map[string]any

type vdfTok int

const (
	tokEOF vdfTok = iota
	tokString
	tokOpen
	tokClose
)

type vdfLexer struct {
	src []byte
	pos int
}

func parseVDF(raw []byte) (vdfMap, error) {
	l := &vdfLexer{src: raw}
	root := vdfMap{}
	for {
		tok, key, err := l.next()
		if err != nil {
			return nil, err
		}
		switch tok {
		case tokEOF:
			return root, nil
		case tokString:
			val, err := l.parseValue()
			if err != nil {
				return nil, err
			}
			root[key] = val
		default:
			return nil, fmt.Errorf("VDF: неожиданная скобка на позиции %d", l.pos)
		}
	}
}

// parseValue читает то, что стоит после ключа: либо строку, либо вложенный блок.
func (l *vdfLexer) parseValue() (any, error) {
	tok, val, err := l.next()
	if err != nil {
		return nil, err
	}
	switch tok {
	case tokString:
		return val, nil
	case tokOpen:
		return l.parseMap()
	default:
		return nil, fmt.Errorf("VDF: у ключа нет значения на позиции %d", l.pos)
	}
}

func (l *vdfLexer) parseMap() (vdfMap, error) {
	m := vdfMap{}
	for {
		tok, key, err := l.next()
		if err != nil {
			return nil, err
		}
		switch tok {
		case tokClose:
			return m, nil
		case tokEOF:
			return nil, fmt.Errorf("VDF: незакрытая скобка")
		case tokString:
			val, err := l.parseValue()
			if err != nil {
				return nil, err
			}
			m[key] = val
		default:
			return nil, fmt.Errorf("VDF: лишняя открывающая скобка на позиции %d", l.pos)
		}
	}
}

func (l *vdfLexer) next() (vdfTok, string, error) {
	l.skipSpaceAndComments()
	if l.pos >= len(l.src) {
		return tokEOF, "", nil
	}
	switch c := l.src[l.pos]; c {
	case '{':
		l.pos++
		return tokOpen, "", nil
	case '}':
		l.pos++
		return tokClose, "", nil
	case '"':
		s, err := l.quoted()
		return tokString, s, err
	default:
		return tokString, l.bare(), nil
	}
}

func (l *vdfLexer) skipSpaceAndComments() {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			l.pos++
		case c == '/' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '/':
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.pos++
			}
		default:
			return
		}
	}
}

// quoted читает строку в кавычках, разворачивая экранирование: пути Windows в
// libraryfolders.vdf записаны как C:\\Program Files\\Steam.
func (l *vdfLexer) quoted() (string, error) {
	l.pos++ // открывающая кавычка
	var out []byte
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch c {
		case '"':
			l.pos++
			return string(out), nil
		case '\\':
			l.pos++
			if l.pos >= len(l.src) {
				return "", fmt.Errorf("VDF: обрыв на экранирующем слэше")
			}
			switch l.src[l.pos] {
			case 'n':
				out = append(out, '\n')
			case 't':
				out = append(out, '\t')
			default:
				out = append(out, l.src[l.pos])
			}
			l.pos++
		default:
			out = append(out, c)
			l.pos++
		}
	}
	return "", fmt.Errorf("VDF: незакрытая кавычка")
}

// bare читает токен без кавычек — Steam иногда пишет ключи именно так.
func (l *vdfLexer) bare() string {
	start := l.pos
	for l.pos < len(l.src) {
		switch l.src[l.pos] {
		case ' ', '\t', '\r', '\n', '{', '}', '"':
			return string(l.src[start:l.pos])
		default:
			l.pos++
		}
	}
	return string(l.src[start:l.pos])
}
