package ottrecql

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseError is a parse error at a specific offset in the input.
type ParseError struct {
	Pos
	Message string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("parse error at offset %d: %s", e.Offset, e.Message)
}

// TODO: error pretty-print

// Parse parses a query expression and returns the root AST node.
func Parse(input string) (Node, error) {
	p := &parser{tok: NewTokenizer(input)}
	expr, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	if p.tok.Token() != TokenEOF {
		return nil, &ParseError{
			Pos:     p.tok.Pos(),
			Message: fmt.Sprintf("unexpected token %q", p.tok.Text()),
		}
	}
	return expr, nil
}

type parser struct {
	tok *Tokenizer
}

// binding powers for infix operators (higher = tighter)
const (
	bpOr  = 1
	bpAnd = 3
)

// infixBP gets the binding power of tok.
func infixBP(tok TokenType) int {
	switch tok {
	case TokenOr:
		return bpOr
	case TokenAnd:
		return bpAnd
	}
	return 0
}

// parseExpr is the Pratt-style expression parser.
// It consumes infix operators whose binding power exceeds minBP.
func (p *parser) parseExpr(minBP int) (Node, error) {
	left, err := p.parsePrefix()
	if err != nil {
		return nil, err
	}
	for {
		bp := infixBP(p.tok.Token())
		if bp <= minBP {
			break
		}
		pos := p.tok.Pos()
		opTok := p.tok.Token()
		p.tok.Next()
		// left-associative: recurse with same power so equal-precedence operators stop
		right, err := p.parseExpr(bp)
		if err != nil {
			return nil, err
		}
		switch opTok {
		case TokenAnd:
			left = &AndNode{Pos: pos, Left: left, Right: right}
		case TokenOr:
			left = &OrNode{Pos: pos, Left: left, Right: right}
		}
	}
	return left, nil
}

// parsePrefix handles prefix expressions: unary NOT, grouped expressions, and calls.
func (p *parser) parsePrefix() (Node, error) {
	pos := p.tok.Pos()
	switch p.tok.Token() {
	case TokenNot:
		p.tok.Next()
		// not binds tighter than and; recurse at bpAnd so and/or are not consumed
		expr, err := p.parseExpr(bpAnd)
		if err != nil {
			return nil, err
		}
		return &NotNode{Pos: pos, Expr: expr}, nil

	case TokenLParen:
		p.tok.Next()
		expr, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		if err := p.expect(TokenRParen, "group closing )"); err != nil {
			return nil, err
		}
		return expr, nil

	case TokenKeyword:
		return p.parseMatch()

	default:
		return nil, &ParseError{
			Pos:     pos,
			Message: fmt.Sprintf("unexpected token %q", p.tok.Text()),
		}
	}
}

func (p *parser) expect(tok TokenType, what string) error {
	if p.tok.Token() != tok {
		return &ParseError{
			Pos:     p.tok.Pos(),
			Message: fmt.Sprintf("expected %s, got %q", what, p.tok.Text()),
		}
	}
	p.tok.Next()
	return nil
}

// parseMatch parses a match function.
func (p *parser) parseMatch() (Node, error) {
	pos := p.tok.Pos()
	name := p.tok.Text()
	p.tok.Next()
	if err := p.expect(TokenLParen, "opening ( after match function name"); err != nil {
		return nil, err
	}
	var (
		expr Node
		err  error
	)
	switch name {
	case "schdate":
		expr, err = p.parseMatchSchDate(pos)
	case "time":
		expr, err = p.parseMatchTime(pos)
	case "facility":
		expr, err = p.parseMatchFacility(pos)
	case "activity":
		expr, err = p.parseMatchActivity(pos)
	case "latlng":
		expr, err = p.parseMatchLatLng(pos)
	default:
		return nil, &ParseError{Pos: pos, Message: fmt.Sprintf("unknown match function %q", name)}
	}
	if err != nil {
		return nil, err
	}
	if err := p.expect(TokenRParen, "closing ) after match function arguments"); err != nil {
		return nil, err
	}
	return expr, nil
}

// parseMatchSchDate parses the body of schdate(date).
func (p *parser) parseMatchSchDate(pos Pos) (Node, error) {
	d, err := p.parseDate()
	if err != nil {
		return nil, err
	}
	return &SchDateNode{Pos: pos, Date: d}, nil
}

// parseMatchTime parses the body of time([weekday...|date...] @ [time...|timerange...]).
func (p *parser) parseMatchTime(pos Pos) (Node, error) {
	expr := &TimeNode{Pos: pos}

	// parse day specs (weekday or date tokens), comma-separated (comma optional)
	for p.tok.Token() == TokenWeekday || p.tok.Token() == TokenDate {
		dayPos := p.tok.Pos()
		if p.tok.Token() == TokenWeekday {
			wd, ok := parseWeekday(strings.ToLower(p.tok.Text()))
			if !ok {
				return nil, &ParseError{Pos: dayPos, Message: fmt.Sprintf("unknown weekday %q", p.tok.Text())}
			}
			expr.Days = append(expr.Days, WeekdayLit{Pos: dayPos, Weekday: wd})
			p.tok.Next()
		} else {
			d, err := p.parseDate()
			if err != nil {
				return nil, err
			}
			expr.Days = append(expr.Days, d)
		}
		if p.tok.Token() == TokenComma {
			p.tok.Next()
		}
	}

	// parse @ and time specs.
	if p.tok.Token() == TokenAt {
		p.tok.Next()
		for p.tok.Token() == TokenTime {
			ts, err := p.parseTimeSpec()
			if err != nil {
				return nil, err
			}
			expr.Times = append(expr.Times, ts)
			if p.tok.Token() == TokenComma {
				p.tok.Next()
			}
		}
	} else if len(expr.Days) == 0 && p.tok.Token() == TokenTime {
		// no day specs and no @, so treat remaining tokens as time specs
		for p.tok.Token() == TokenTime {
			ts, err := p.parseTimeSpec()
			if err != nil {
				return nil, err
			}
			expr.Times = append(expr.Times, ts)
			if p.tok.Token() == TokenComma {
				p.tok.Next()
			}
		}
	}

	return expr, nil
}

// parseMatchFacility parses the body of facility([string...]).
func (p *parser) parseMatchFacility(pos Pos) (Node, error) {
	strs, err := p.parseStringList("list of facility name substrings")
	if err != nil {
		return nil, err
	}
	return &FacilityNode{Pos: pos, Strings: strs}, nil
}

// parseMatchActivity parses the body of activity([string...]).
func (p *parser) parseMatchActivity(pos Pos) (Node, error) {
	strs, err := p.parseStringList("list of activity name substrings")
	if err != nil {
		return nil, err
	}
	return &ActivityNode{Pos: pos, Strings: strs}, nil
}

// parseMatchLatLng parses the body of latlng(number, number, number).
func (p *parser) parseMatchLatLng(pos Pos) (Node, error) {
	lat, err := p.parseNumber("latitude")
	if err != nil {
		return nil, err
	}
	if err := p.expect(TokenComma, "comma"); err != nil {
		return nil, err
	}
	lng, err := p.parseNumber("longitude")
	if err != nil {
		return nil, err
	}
	if err := p.expect(TokenComma, "comma"); err != nil {
		return nil, err
	}
	km, err := p.parseNumber("distance in km")
	if err != nil {
		return nil, err
	}
	return &LatLngNode{Pos: pos, Lat: lat, Lng: lng, Dist: km}, nil
}

// parseStringList parses the arguments of a match function taking a list of
// one or more strings.
func (p *parser) parseStringList(of string) ([]string, error) {
	var result []string
	for p.tok.Token() == TokenString {
		s, err := strconv.Unquote(p.tok.Text())
		if err != nil {
			return nil, &ParseError{
				Pos:     p.tok.Pos(),
				Message: fmt.Sprintf("invalid string %q in %s: %v", p.tok.Text(), of, err),
			}
		}
		result = append(result, s)
		p.tok.Next()
		if p.tok.Token() == TokenComma {
			p.tok.Next()
		} else {
			break
		}
	}
	if len(result) == 0 {
		return result, fmt.Errorf("expected at least one string in %s", of)
	}
	return result, nil
}

// parseNumber parses an optionally-negated float32.
func (p *parser) parseNumber(what string) (float32, error) {
	pos := p.tok.Pos()
	neg := false
	if p.tok.Token() == TokenDash {
		neg = true
		p.tok.Next()
		pos = p.tok.Pos()
	}
	if p.tok.Token() != TokenNumber {
		return 0, &ParseError{
			Pos:     pos,
			Message: fmt.Sprintf("expected number (%s), got %q", what, p.tok.Text()),
		}
	}
	text := p.tok.Text()
	p.tok.Next()
	f, err := strconv.ParseFloat(text, 32)
	if err != nil {
		return 0, &ParseError{Pos: pos, Message: fmt.Sprintf("invalid number %q (%s): %v", text, what, err)}
	}
	if neg {
		f = -f
	}
	return float32(f), nil
}

// parseDate consumes a TokenDate and returns a Date value.
func (p *parser) parseDate() (DateLit, error) {
	pos := p.tok.Pos()
	if p.tok.Token() != TokenDate {
		return DateLit{}, &ParseError{
			Pos:     pos,
			Message: fmt.Sprintf("expected date, got %q", p.tok.Text()),
		}
	}
	text := p.tok.Text()
	p.tok.Next()
	if strings.EqualFold(text, "today") {
		return DateLit{Pos: pos, IsToday: true}, nil
	}
	p0, p1, ok1 := strings.Cut(text, "-")
	p1, p2, ok2 := strings.Cut(p1, "-")
	p2, _, nok3 := strings.Cut(p2, "-")
	if !ok1 || !ok2 || nok3 {
		return DateLit{}, &ParseError{Pos: pos, Message: fmt.Sprintf("invalid date %q", text)}
	}
	year, e1 := strconv.Atoi(p0)
	month, e2 := strconv.Atoi(p1)
	day, e3 := strconv.Atoi(p2)
	if e1 != nil || e2 != nil || e3 != nil {
		return DateLit{}, &ParseError{Pos: pos, Message: fmt.Sprintf("invalid date %q", text)}
	}
	return DateLit{Pos: pos, Year: year, Month: time.Month(month), Day: day}, nil
}

// parseTime consumes a TokenTime and returns a Time value.
func (p *parser) parseTime() (TimeLit, error) {
	pos := p.tok.Pos()
	if p.tok.Token() != TokenTime {
		return TimeLit{}, &ParseError{
			Pos:     pos,
			Message: fmt.Sprintf("expected time, got %q", p.tok.Text()),
		}
	}
	text := p.tok.Text()
	p.tok.Next()
	if strings.EqualFold(text, "now") {
		return TimeLit{Pos: pos, IsNow: true}, nil
	}
	t := TimeLit{Pos: pos}
	lower := strings.ToLower(text)
	switch {
	case strings.HasSuffix(lower, "pm"):
		t.HasPeriod = true
		t.PM = true
		text = text[:len(text)-2]
	case strings.HasSuffix(lower, "am"):
		t.HasPeriod = true
		text = text[:len(text)-2]
	case strings.HasSuffix(lower, "p"):
		t.HasPeriod = true
		t.PM = true
		text = text[:len(text)-1]
	case strings.HasSuffix(lower, "a"):
		t.HasPeriod = true
		text = text[:len(text)-1]
	}
	hourStr, minuteStr, ok := strings.Cut(text, ":")
	if !ok {
		return TimeLit{}, &ParseError{Pos: pos, Message: fmt.Sprintf("invalid time %q", text)}
	}
	hour, e1 := strconv.Atoi(hourStr)
	minute, e2 := strconv.Atoi(minuteStr)
	if e1 != nil || e2 != nil {
		return TimeLit{}, &ParseError{Pos: pos, Message: fmt.Sprintf("invalid time %q", text)}
	}
	t.Hour = hour
	t.Minute = minute
	return t, nil
}

// parseTimeSpec parses a single time or a time range (time - time).
func (p *parser) parseTimeSpec() (TimeSpec, error) {
	t, err := p.parseTime()
	if err != nil {
		return nil, err
	}
	if p.tok.Token() == TokenDash {
		p.tok.Next()
		end, err := p.parseTime()
		if err != nil {
			return nil, err
		}
		return TimeRangeLit{Start: t, End: end}, nil
	}
	return t, nil
}

// TODO: validate time/date/latlng
