package ottrecql

import (
	"fmt"
	"strings"
	"time"
)

// TokenType is the type of a lexical token.
type TokenType int

const (
	TokenInvalid TokenType = iota // invalid/unrecognized token
	TokenEOF                      // end of input

	TokenLParen  // (
	TokenRParen  // )
	TokenComma   // ,
	TokenAt      // @ (used to separate days/dates and times/ranges in the time function)
	TokenDash    // - (used in time ranges and negative numbers)
	TokenNot     // not, !
	TokenAnd     // and, &&
	TokenOr      // or, ||
	TokenString  // double-quoted string with backslash escapes
	TokenDate    // YYYY-MM-DD, today
	TokenTime    // HH:MM[a|am|p|pm], now
	TokenWeekday // weekday name (2-letter, 3-letter, or full)
	TokenNumber  // numeric literal (float32)
	TokenKeyword // lowercase alphanumeric identifier (i.e., match function)
)

// String returns the human-readable name of the token type.
func (t TokenType) String() string {
	switch t {
	case TokenInvalid:
		return "invalid"
	case TokenEOF:
		return "EOF"
	case TokenLParen:
		return "("
	case TokenRParen:
		return ")"
	case TokenComma:
		return ","
	case TokenAt:
		return "@"
	case TokenDash:
		return "-"
	case TokenNot:
		return "not"
	case TokenAnd:
		return "and"
	case TokenOr:
		return "or"
	case TokenString:
		return "string"
	case TokenDate:
		return "date"
	case TokenTime:
		return "time"
	case TokenWeekday:
		return "weekday"
	case TokenNumber:
		return "number"
	case TokenKeyword:
		return "keyword"
	default:
		return fmt.Sprintf("TokenType(%d)", int(t))
	}
}

// Tokenizer scans an input string into tokens.
type Tokenizer struct {
	str   string
	pos   int
	start int
	end   int
	tok   TokenType
}

// NewTokenizer creates a Tokenizer and advances to the first token.
func NewTokenizer(s string) *Tokenizer {
	t := &Tokenizer{str: s}
	t.Next()
	return t
}

// Token returns the current token type.
func (t *Tokenizer) Token() TokenType { return t.tok }

// Offset returns the byte offset of the current token in the input.
func (t *Tokenizer) Offset() int { return t.start }

// Len returns the byte length of the current token.
func (t *Tokenizer) Len() int { return t.end - t.start }

// Text returns the raw text of the current token.
func (t *Tokenizer) Text() string { return t.str[t.start:t.end] }

// Next advances to the next token. If invalid, the token is set to
// [TokenInvalid]. It always moves forwards, setting [TokenEOF] at the end of
// the input.
func (t *Tokenizer) Next() {
	for t.pos < len(t.str) && isSpace(t.str[t.pos]) {
		t.pos++
	}
	t.start = t.pos
	if t.pos >= len(t.str) {
		t.tok = TokenEOF
		t.end = t.pos
		return
	}
	ch := t.str[t.pos]
	switch {
	case ch == '(':
		t.pos++
		t.tok = TokenLParen
	case ch == ')':
		t.pos++
		t.tok = TokenRParen
	case ch == ',':
		t.pos++
		t.tok = TokenComma
	case ch == '@':
		t.pos++
		t.tok = TokenAt
	case ch == '-':
		t.pos++
		t.tok = TokenDash
	case ch == '!':
		t.pos++
		t.tok = TokenNot
	case ch == '&' && t.pos+1 < len(t.str) && t.str[t.pos+1] == '&':
		t.pos += 2
		t.tok = TokenAnd
	case ch == '|' && t.pos+1 < len(t.str) && t.str[t.pos+1] == '|':
		t.pos += 2
		t.tok = TokenOr
	case ch == '"':
		t.scanString()
	case isDigit(ch):
		t.scanNumeric()
	case isLetter(ch):
		t.scanIdent()
	default:
		t.pos++
		t.tok = TokenInvalid
	}
	t.end = t.pos
}

// scanString consumes a double-quoted string with backslash escapes.
func (t *Tokenizer) scanString() {
	t.pos++ // consume opening "
	for t.pos < len(t.str) {
		ch := t.str[t.pos]
		t.pos++
		switch ch {
		case '\\':
			t.pos++ // skip escaped character
		case '"':
			t.tok = TokenString
			return
		}
	}
	t.tok = TokenInvalid
}

// scanNumeric consumes a token starting with a digit.
func (t *Tokenizer) scanNumeric() {
	// count leading digits
	p := t.pos
	for p < len(t.str) && isDigit(t.str[p]) {
		p++
	}
	nDigits := p - t.pos

	// date (YYYY-MM-DD)
	if nDigits == 4 && p < len(t.str) && t.str[p] == '-' {
		q := p + 1
		mm := 0
		for q < len(t.str) && isDigit(t.str[q]) {
			mm++
			q++
		}
		if mm == 2 && q < len(t.str) && t.str[q] == '-' {
			q++
			dd := 0
			for q < len(t.str) && isDigit(t.str[q]) {
				dd++
				q++
			}
			if dd == 2 {
				t.pos = q
				t.tok = TokenDate
				return
			}
		}
	}

	// time (H:MM or HH:MM with optional am/pm suffix)
	if nDigits >= 1 && nDigits <= 2 && p < len(t.str) && t.str[p] == ':' {
		q := p + 1
		mn := 0
		for q < len(t.str) && isDigit(t.str[q]) {
			mn++
			q++
		}
		if mn == 2 {
			t.pos = q
			t.scanTimePeriod()
			t.tok = TokenTime
			return
		}
	}

	// number
	t.pos = p
	if t.pos < len(t.str) && t.str[t.pos] == '.' {
		t.pos++
		for t.pos < len(t.str) && isDigit(t.str[t.pos]) {
			t.pos++
		}
	}
	t.tok = TokenNumber
}

// scanTimePeriod consumes an optional a/am/p/pm suffix, but only when followed
// by a word boundary.
func (t *Tokenizer) scanTimePeriod() {
	if t.pos >= len(t.str) {
		return
	}
	ch := t.str[t.pos]
	if ch != 'a' && ch != 'A' && ch != 'p' && ch != 'P' {
		return
	}
	save := t.pos
	t.pos++
	if t.pos < len(t.str) && (t.str[t.pos] == 'm' || t.str[t.pos] == 'M') {
		t.pos++
	}
	// require word boundary, backtrack if not
	if t.pos < len(t.str) && isAlphaNum(t.str[t.pos]) {
		t.pos = save
	}
}

// scanIdent sets the token type to a specific kind of identifier if known,
// otherwise [TokenKeyword].
func (t *Tokenizer) scanIdent() {
	for t.pos < len(t.str) && isAlphaNum(t.str[t.pos]) {
		t.pos++
	}
	switch lower := strings.ToLower(t.str[t.start:t.pos]); lower {
	case "today":
		t.tok = TokenDate
	case "now":
		t.tok = TokenTime
	default:
		if tok, ok := parseOpWord(lower); ok {
			t.tok = tok
		} else if _, ok := parseWeekday(lower); ok {
			t.tok = TokenWeekday
		} else {
			t.tok = TokenKeyword
		}
	}
}

// parseOpWord parses a lowercase operator word.
func parseOpWord(s string) (TokenType, bool) {
	switch s {
	case "not":
		return TokenNot, true
	case "and":
		return TokenAnd, true
	case "or":
		return TokenOr, true
	default:
		return TokenInvalid, false
	}
}

// parseWeekday parses a lowercase two-letter, three-letter, or full weekday.
func parseWeekday(s string) (time.Weekday, bool) {
	switch s {
	case "monday", "mon", "mo":
		return time.Monday, true
	case "tuesday", "tue", "tu":
		return time.Tuesday, true
	case "wednesday", "wed", "we":
		return time.Wednesday, true
	case "thursday", "thu", "th":
		return time.Thursday, true
	case "friday", "fri", "fr":
		return time.Friday, true
	case "saturday", "sat", "sa":
		return time.Saturday, true
	case "sunday", "sun", "su":
		return time.Sunday, true
	default:
		return 0, false
	}
}

func isSpace(ch byte) bool    { return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' }
func isDigit(ch byte) bool    { return ch >= '0' && ch <= '9' }
func isLetter(ch byte) bool   { return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') }
func isAlphaNum(ch byte) bool { return isLetter(ch) || isDigit(ch) }
