package inspector

import "strings"

// Normalize turns a SQL string into the shape that N+1 detection groups by:
// literals and numbers become ?, comments go, whitespace runs become one
// space, `in (?, ?, ?)` becomes `in (?)`, and repeated value groups collapse
// to one. Identifiers and keyword case stay as they are, because one caller
// sends the same text every time.
func Normalize(sql string) string {
	n := normalizer{src: sql, out: make([]byte, 0, len(sql))}
	n.run()
	return string(n.out)
}

type normalizer struct {
	src    string
	i      int
	out    []byte
	space  bool  // whitespace or a comment was skipped since the last token
	parens []int // out offsets of the open parentheses in code
}

func (n *normalizer) run() {
	for n.i < len(n.src) {
		c := n.src[n.i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f':
			n.space = true
			n.i++
		case c == '-' && n.peek(1) == '-', c == '#' && n.peek(1) != '>':
			n.skipLine()
		case c == '/' && n.peek(1) == '*':
			n.skipBlock()
		case c == '\'':
			n.skipQuoted('\'', true)
			n.emit("?")
		case c == '"' || c == '`':
			start := n.i
			n.skipQuoted(c, false)
			n.emit(n.src[start:n.i])
		case c == '$':
			n.dollar()
		case c == ':':
			n.colon()
		case isDigit(c) && !n.afterIdent():
			n.skipNumber()
			n.emit("?")
		case c == '(':
			n.i++
			n.emit("(")
			n.parens = append(n.parens, len(n.out)-1)
		case c == ')':
			n.i++
			n.emit(")")
			n.closeParen()
		default:
			start := n.i
			for n.i < len(n.src) && isIdent(n.src[n.i]) {
				n.i++
			}
			if n.i == start {
				n.i++
			}
			n.emit(n.src[start:n.i])
		}
	}
}

func (n *normalizer) peek(k int) byte {
	if n.i+k < len(n.src) {
		return n.src[n.i+k]
	}
	return 0
}

// emit writes a token, with one space before it when the source had any.
func (n *normalizer) emit(tok string) {
	if n.space && len(n.out) > 0 {
		n.out = append(n.out, ' ')
	}
	n.space = false
	n.out = append(n.out, tok...)
}

func (n *normalizer) afterIdent() bool {
	if n.space || len(n.out) == 0 {
		return false
	}
	return isIdent(n.out[len(n.out)-1])
}

func (n *normalizer) skipLine() {
	for n.i < len(n.src) && n.src[n.i] != '\n' {
		n.i++
	}
	n.space = true
}

func (n *normalizer) skipBlock() {
	end := strings.Index(n.src[n.i+2:], "*/")
	if end < 0 {
		n.i = len(n.src)
	} else {
		n.i += 2 + end + 2
	}
	n.space = true
}

// skipQuoted moves past a quoted string or identifier. A doubled quote is an
// escaped quote. Backslash escapes apply to strings only (MySQL).
func (n *normalizer) skipQuoted(q byte, backslash bool) {
	n.i++
	for n.i < len(n.src) {
		c := n.src[n.i]
		switch {
		case backslash && c == '\\':
			n.i += 2
		case c == q && n.peek(1) == q:
			n.i += 2
		case c == q:
			n.i++
			return
		default:
			n.i++
		}
	}
}

func (n *normalizer) skipNumber() {
	if n.src[n.i] == '0' && (n.peek(1) == 'x' || n.peek(1) == 'X') {
		n.i += 2
		for n.i < len(n.src) && isHex(n.src[n.i]) {
			n.i++
		}
		return
	}
	for n.i < len(n.src) && (isDigit(n.src[n.i]) || n.src[n.i] == '.') {
		n.i++
	}
	if n.i < len(n.src) && (n.src[n.i] == 'e' || n.src[n.i] == 'E') {
		j := n.i + 1
		if j < len(n.src) && (n.src[j] == '+' || n.src[j] == '-') {
			j++
		}
		if j < len(n.src) && isDigit(n.src[j]) {
			n.i = j
			for n.i < len(n.src) && isDigit(n.src[n.i]) {
				n.i++
			}
		}
	}
}

// dollar handles $1 placeholders and $tag$…$tag$ quoted strings (PostgreSQL).
func (n *normalizer) dollar() {
	if isDigit(n.peek(1)) {
		n.i++
		for n.i < len(n.src) && isDigit(n.src[n.i]) {
			n.i++
		}
		n.emit("?")
		return
	}
	j := n.i + 1
	for j < len(n.src) && isIdent(n.src[j]) && n.src[j] != '$' {
		j++
	}
	if j < len(n.src) && n.src[j] == '$' {
		tag := n.src[n.i : j+1]
		end := strings.Index(n.src[j+1:], tag)
		if end < 0 {
			n.i = len(n.src)
		} else {
			n.i = j + 1 + end + len(tag)
		}
		n.emit("?")
		return
	}
	n.i++
	n.emit("$")
}

// colon keeps :: casts and turns :name (PDO named) into ?.
func (n *normalizer) colon() {
	if n.peek(1) == ':' {
		n.i += 2
		n.emit("::")
		return
	}
	if c := n.peek(1); isIdent(c) && c != '$' {
		n.i++
		for n.i < len(n.src) && isIdent(n.src[n.i]) {
			n.i++
		}
		n.emit("?")
		return
	}
	n.i++
	n.emit(":")
}

// closeParen collapses the group that the ) just closed when it holds only
// placeholders: after `in` it becomes (?), and a group equal to the group
// before it (`values (?, ?), (?, ?)`) is dropped with its comma.
func (n *normalizer) closeParen() {
	if len(n.parens) == 0 {
		return
	}
	open := n.parens[len(n.parens)-1]
	n.parens = n.parens[:len(n.parens)-1]
	inner := n.out[open+1 : len(n.out)-1]
	count := 0
	for _, c := range inner {
		switch c {
		case '?':
			count++
		case ',', ' ':
		default:
			return
		}
	}
	if count == 0 {
		return
	}
	if precededByIn(n.out[:open]) {
		count = 1
	}
	group := "(" + strings.Repeat("?, ", count-1) + "?)"
	n.out = append(n.out[:open], group...)

	head := strings.TrimRight(string(n.out[:open]), " ")
	if prev, ok := strings.CutSuffix(head, ","); ok {
		if strings.HasSuffix(strings.TrimRight(prev, " "), group) {
			n.out = []byte(strings.TrimRight(prev, " "))
		}
	}
}

func precededByIn(b []byte) bool {
	s := strings.TrimRight(string(b), " ")
	if len(s) < 2 || !strings.EqualFold(s[len(s)-2:], "in") {
		return false
	}
	return len(s) == 2 || !isIdent(s[len(s)-3])
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isHex(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func isIdent(c byte) bool {
	return c == '_' || c == '$' || isDigit(c) || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}
