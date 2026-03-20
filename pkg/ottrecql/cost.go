package ottrecql

// Cost calculates a relative cost value for an expression.
func Cost(e Node) int {
	switch e := e.(type) {
	case *NotNode:
		return ownCost(e) + Cost(e.Expr)
	case *AndNode:
		return ownCost(e) + Cost(e.Left) + Cost(e.Right)
	case *OrNode:
		return ownCost(e) + Cost(e.Left) + Cost(e.Right)
	case *SchDateNode:
		return ownCost(e)
	case *TimeNode:
		return ownCost(e)
	case *FacilityNode:
		return ownCost(e)
	case *ActivityNode:
		return ownCost(e)
	case *LatLngNode:
		return ownCost(e)
	default:
		panic("invalid node")
	}
}

func ownCost(e Node) int {
	switch e := e.(type) {
	case *NotNode:
		return 1
	case *AndNode:
		return 1
	case *OrNode:
		return 1
	case *SchDateNode:
		return 48
	case *TimeNode:
		var c int
		for _, d := range e.Days {
			switch d.(type) {
			case WeekdayLit:
				c += 1
			case DateLit:
				c += 3
			default:
				panic("invalid node")
			}
		}
		for _, t := range e.Times {
			switch t.(type) {
			case TimeLit:
				c += 3
			case TimeRangeLit:
				c += 6
			default:
				panic("invalid node")
			}
		}
		return 1 + 32*c // since we need to run it over all times in a schedule
	case *FacilityNode:
		var c int
		for _, s := range e.FuzzyName {
			if isASCII(s) {
				c += len(s) * 2
			} else {
				c += len(s) * 4
			}
		}
		return 1 + c
	case *ActivityNode:
		var c int
		for _, s := range e.FuzzyName {
			if isASCII(s) {
				c += len(s) * 2
			} else {
				c += len(s) * 4
			}
		}
		return 1 + c
	case *LatLngNode:
		return 8
	default:
		panic("invalid node")
	}
}
