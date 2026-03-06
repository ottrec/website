package ottrecql

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseError is a parse error at a specific offset in the input.
type ParseError struct {
	Offset  int
	Message string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("parse error at offset %d: %s", e.Offset, e.Message)
}

// TODO: error pretty-print

// Parse parses a query expression and returns the root AST node.
func Parse(input string) (Expr, error) {
	p := &parser{tok: NewTokenizer(input)}
	expr, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	if p.tok.Token() != TokenEOF {
		return nil, &ParseError{
			Offset:  p.tok.Offset(),
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
func (p *parser) parseExpr(minBP int) (Expr, error) {
	left, err := p.parsePrefix()
	if err != nil {
		return nil, err
	}
	for {
		bp := infixBP(p.tok.Token())
		if bp <= minBP {
			break
		}
		offset := p.tok.Offset()
		opTok := p.tok.Token()
		p.tok.Next()
		// left-associative: recurse with same power so equal-precedence operators stop
		right, err := p.parseExpr(bp)
		if err != nil {
			return nil, err
		}
		switch opTok {
		case TokenAnd:
			left = &AndExpr{Offset: offset, Left: left, Right: right}
		case TokenOr:
			left = &OrExpr{Offset: offset, Left: left, Right: right}
		}
	}
	return left, nil
}

// parsePrefix handles prefix expressions: unary NOT, grouped expressions, and calls.
func (p *parser) parsePrefix() (Expr, error) {
	offset := p.tok.Offset()
	switch p.tok.Token() {
	case TokenNot:
		p.tok.Next()
		// not binds tighter than and; recurse at bpAnd so and/or are not consumed
		expr, err := p.parseExpr(bpAnd)
		if err != nil {
			return nil, err
		}
		return &NotExpr{Offset: offset, Expr: expr}, nil

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
			Offset:  offset,
			Message: fmt.Sprintf("unexpected token %q", p.tok.Text()),
		}
	}
}

func (p *parser) expect(tok TokenType, what string) error {
	if p.tok.Token() != tok {
		return &ParseError{
			Offset:  p.tok.Offset(),
			Message: fmt.Sprintf("expected %s, got %q", what, p.tok.Text()),
		}
	}
	p.tok.Next()
	return nil
}

// parseMatch parses a match function.
func (p *parser) parseMatch() (Expr, error) {
	offset := p.tok.Offset()
	name := p.tok.Text()
	p.tok.Next()
	if err := p.expect(TokenLParen, "opening ( after match function name"); err != nil {
		return nil, err
	}
	var (
		expr Expr
		err  error
	)
	switch name {
	case "schdate":
		expr, err = p.parseMatchSchDate(offset)
	case "time":
		expr, err = p.parseMatchTime(offset)
	case "facility":
		expr, err = p.parseMatchFacility(offset)
	case "activity":
		expr, err = p.parseMatchActivity(offset)
	case "latlng":
		expr, err = p.parseMatchLatLng(offset)
	default:
		return nil, &ParseError{Offset: offset, Message: fmt.Sprintf("unknown match function %q", name)}
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
func (p *parser) parseMatchSchDate(offset int) (Expr, error) {
	d, err := p.parseDate()
	if err != nil {
		return nil, err
	}
	return &SchDateExpr{Offset: offset, Date: d}, nil
}

// parseMatchTime parses the body of time([weekday...|date...] @ [time...|timerange...]).
func (p *parser) parseMatchTime(offset int) (Expr, error) {
	expr := &TimeExpr{Offset: offset}

	// parse day specs (weekday or date tokens), comma-separated (comma optional)
	for p.tok.Token() == TokenWeekday || p.tok.Token() == TokenDate {
		dayOffset := p.tok.Offset()
		if p.tok.Token() == TokenWeekday {
			wd, ok := parseWeekday(strings.ToLower(p.tok.Text()))
			if !ok {
				return nil, &ParseError{Offset: dayOffset, Message: fmt.Sprintf("unknown weekday %q", p.tok.Text())}
			}
			expr.Days = append(expr.Days, WeekdaySpec{Offset: dayOffset, Weekday: wd})
			p.tok.Next()
		} else {
			d, err := p.parseDate()
			if err != nil {
				return nil, err
			}
			expr.Days = append(expr.Days, DateSpec{Date: d})
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
func (p *parser) parseMatchFacility(offset int) (Expr, error) {
	strs, err := p.parseStringList("list of facility name substrings")
	if err != nil {
		return nil, err
	}
	return &FacilityExpr{Offset: offset, Strings: strs}, nil
}

// parseMatchActivity parses the body of activity([string...]).
func (p *parser) parseMatchActivity(offset int) (Expr, error) {
	strs, err := p.parseStringList("list of activity name substrings")
	if err != nil {
		return nil, err
	}
	return &ActivityExpr{Offset: offset, Strings: strs}, nil
}

// parseMatchLatLng parses the body of latlng(number, number, number).
func (p *parser) parseMatchLatLng(offset int) (Expr, error) {
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
	return &LatLngExpr{Offset: offset, Lat: lat, Lng: lng, Kilometers: km}, nil
}

// parseStringList parses the arguments of a match function taking a list of
// one or more strings.
func (p *parser) parseStringList(of string) ([]string, error) {
	var result []string
	for p.tok.Token() == TokenString {
		s, err := strconv.Unquote(p.tok.Text())
		if err != nil {
			return nil, &ParseError{
				Offset:  p.tok.Offset(),
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
	offset := p.tok.Offset()
	neg := false
	if p.tok.Token() == TokenDash {
		neg = true
		p.tok.Next()
		offset = p.tok.Offset()
	}
	if p.tok.Token() != TokenNumber {
		return 0, &ParseError{
			Offset:  offset,
			Message: fmt.Sprintf("expected number (%s), got %q", what, p.tok.Text()),
		}
	}
	text := p.tok.Text()
	p.tok.Next()
	f, err := strconv.ParseFloat(text, 32)
	if err != nil {
		return 0, &ParseError{Offset: offset, Message: fmt.Sprintf("invalid number %q (%s): %v", text, what, err)}
	}
	if neg {
		f = -f
	}
	return float32(f), nil
}

// parseDate consumes a TokenDate and returns a Date value.
func (p *parser) parseDate() (Date, error) {
	offset := p.tok.Offset()
	if p.tok.Token() != TokenDate {
		return Date{}, &ParseError{
			Offset:  offset,
			Message: fmt.Sprintf("expected date, got %q", p.tok.Text()),
		}
	}
	text := p.tok.Text()
	p.tok.Next()
	if strings.EqualFold(text, "today") {
		return Date{Offset: offset, IsToday: true}, nil
	}
	parts := strings.SplitN(text, "-", 3)
	if len(parts) != 3 {
		return Date{}, &ParseError{Offset: offset, Message: fmt.Sprintf("invalid date %q", text)}
	}
	year, e1 := strconv.Atoi(parts[0])
	month, e2 := strconv.Atoi(parts[1])
	day, e3 := strconv.Atoi(parts[2])
	if e1 != nil || e2 != nil || e3 != nil {
		return Date{}, &ParseError{Offset: offset, Message: fmt.Sprintf("invalid date %q", text)}
	}
	return Date{Offset: offset, Year: year, Month: time.Month(month), Day: day}, nil
}

// parseTime consumes a TokenTime and returns a Time value.
func (p *parser) parseTime() (Time, error) {
	offset := p.tok.Offset()
	if p.tok.Token() != TokenTime {
		return Time{}, &ParseError{
			Offset:  offset,
			Message: fmt.Sprintf("expected time, got %q", p.tok.Text()),
		}
	}
	text := p.tok.Text()
	p.tok.Next()
	if strings.EqualFold(text, "now") {
		return Time{Offset: offset, IsNow: true}, nil
	}
	t := Time{Offset: offset}
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
	colon := strings.IndexByte(text, ':')
	if colon < 0 {
		return Time{}, &ParseError{Offset: offset, Message: fmt.Sprintf("invalid time %q", text)}
	}
	hour, e1 := strconv.Atoi(text[:colon])
	minute, e2 := strconv.Atoi(text[colon+1:])
	if e1 != nil || e2 != nil {
		return Time{}, &ParseError{Offset: offset, Message: fmt.Sprintf("invalid time %q", text)}
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
		return TimeRange{Start: t, End: end}, nil
	}
	return t, nil
}

// TODO: validate time/date/latlng
