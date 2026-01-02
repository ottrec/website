package ottrecflt

type token int

const (
	// special tokens
	tokenInvalid token = iota
	tokenEOF

	// literals
	tokenString    // double-quoted go string
	tokenDate      // YYYY-MM-DD or today
	tokenTime      // HH:MM (24-hour) or HH:MMa HH:MMp HH:MMam HH:MMpm (12-hour) or now (current time)
	tokenTimeRange // time-time (but now isn't valid)
	tokenWeekday   // two-letter, three-letter, or full weekday names: sunday, monday, tuesday, wednesday, thursday, friday, saturday (case-insensitive)
	tokenNumber    // 32-bit float, not NaN or Inf

	// match functions
	tokenMatchSchdate  // schdate (DATE) - matches schedules valid on the specified date (or which don't have a date range) (parenthesis are optional)
	tokenMatchTime     // time ([WEEKDAY|DATE...] @ [TIME|TIMERANGE...]) - matches activities on the specified weekdays/dates and/or times/ranges (matches any combination of date and time) (space-separated) (@ is only required if including both dates and times)
	tokenMatchFacility // facility (STRING, ...) - matches facilities with a name including any of the specified double-quoted substrings (case-insensitive, normalized, fuzzy) (comma-separated) (parenthesis are optional if only one)
	tokenMatchActivity // activity (STRING, ...) - matches activities with a name including any of the specified double-quoted substrings (case-insensitive, normalized, fuzzy) (comma-separated) (parenthesis are optional if only one)
	tokenMatchLngLat   // lnglat (lng, lat, km) - matches facilities the specified distance from the provided longitude/latitude values

	// operators
	tokenLParen // (
	tokenRParen // )
	tokenNot    // not !
	tokenAnd    // and &&
	tokenOr     // or ||

	// misc
	tokenAt // @ (used for the time match function)

	maxToken
)

func (t token) String() string {
	switch t {
	case tokenInvalid:
		return "invalid"
	case tokenEOF:
		return "eof"
	case tokenString:
		return "string"
	case tokenDate:
		return "date"
	case tokenTime:
		return "time"
	case tokenTimeRange:
		return "timerange"
	case tokenWeekday:
		return "weekday"
	case tokenNumber:
		return "number"
	case tokenMatchSchdate:
		return "schdate()"
	case tokenMatchTime:
		return "time()"
	case tokenMatchFacility:
		return "facility()"
	case tokenMatchActivity:
		return "activity()"
	case tokenMatchLngLat:
		return "lnglat()"
	case tokenLParen:
		return "lparen"
	case tokenRParen:
		return "rparen"
	case tokenNot:
		return "not"
	case tokenAnd:
		return "and"
	case tokenOr:
		return "or"
	case tokenAt:
		return "at"
	}
	return ""
}
