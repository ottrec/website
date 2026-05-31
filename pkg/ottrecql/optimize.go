package ottrecql

import (
	"cmp"
	"slices"
)

// Optimize returns a new AST with the following transformations:
//   - AND/OR chains flattened and all operands sorted by ascending cost
//   - not(not(x)) -> x
//   - facility("a") or facility("b") -> facility("a","b")
//   - activity("a") or activity("b") -> activity("a","b")
//   - time(mon @ 10am) or time(tue @ 10am) -> time(mon,tue @ 10am)
//   - time(mon @ 10am) or time(mon @ 11am) -> time(mon @ 10am,11am)
func Optimize(e Node) Node {
	switch e := e.(type) {
	case *NotNode:
		inner := Optimize(e.Expr)
		if n, ok := inner.(*NotNode); ok {
			return n.Expr
		}
		return &NotNode{Pos: e.Pos, Expr: inner}
	case *AndNode:
		ops := collectAnds(e)
		for i, op := range ops {
			ops[i] = Optimize(op)
		}
		slices.SortStableFunc(ops, byCost)
		return foldLeft(ops, e.Pos, func(pos Pos, l, r Node) Node {
			return &AndNode{Pos: pos, Left: l, Right: r}
		})
	case *OrNode:
		ops := collectOrs(e)
		for i, op := range ops {
			ops[i] = Optimize(op)
		}
		ops = mergeOrOps(ops)
		slices.SortStableFunc(ops, byCost)
		return foldLeft(ops, e.Pos, func(pos Pos, l, r Node) Node {
			return &OrNode{Pos: pos, Left: l, Right: r}
		})
	default:
		return e
	}
}

func byCost(a, b Node) int {
	return cmp.Compare(Cost(a), Cost(b))
}

// collectAnds flattens an AND tree into a flat slice of operands.
func collectAnds(e Node) []Node {
	n, ok := e.(*AndNode)
	if !ok {
		return []Node{e}
	}
	return append(collectAnds(n.Left), collectAnds(n.Right)...)
}

// collectOrs flattens an OR tree into a flat slice of operands.
func collectOrs(e Node) []Node {
	n, ok := e.(*OrNode)
	if !ok {
		return []Node{e}
	}
	return append(collectOrs(n.Left), collectOrs(n.Right)...)
}

// foldLeft builds a left-associative binary tree from a flat list of operands.
func foldLeft(ops []Node, pos Pos, mk func(Pos, Node, Node) Node) Node {
	result := ops[0]
	for _, op := range ops[1:] {
		result = mk(pos, result, op)
	}
	return result
}

// mergeOrOps collapses same-type leaf nodes within a flat OR operand list. All
// FacilityNodes/ActivityNodes are combined into one, and TimeNodes are merged
// where their days or times are structurally equal.
func mergeOrOps(ops []Node) []Node {
	var (
		fac       *FacilityNode
		act       *ActivityNode
		timeNodes []*TimeNode
		rest      []Node
	)
	for _, op := range ops {
		switch op := op.(type) {
		case *FacilityNode:
			if fac == nil {
				fac = &FacilityNode{Pos: op.Pos}
			}
			fac.FuzzyName = append(fac.FuzzyName, op.FuzzyName...)
		case *ActivityNode:
			if act == nil {
				act = &ActivityNode{Pos: op.Pos}
			}
			act.FuzzyName = append(act.FuzzyName, op.FuzzyName...)
		case *TimeNode:
			timeNodes = append(timeNodes, op)
		default:
			rest = append(rest, op)
		}
	}
	result := rest
	if fac != nil {
		result = append(result, fac)
	}
	if act != nil {
		result = append(result, act)
	}
	return append(result, mergeTimeNodes(timeNodes)...)
}

// mergeTimeNodes merges TimeNodes where possible, trying each new node against
// all already-accumulated nodes.
func mergeTimeNodes(nodes []*TimeNode) []Node {
	acc := make([]*TimeNode, 0, len(nodes))
	for _, n := range nodes {
		var absorbed bool
		for i, m := range acc {
			if r := mergeTimeOr(m, n); r != nil {
				acc[i] = r
				absorbed = true
				break
			}
		}
		if !absorbed {
			acc = append(acc, n)
		}
	}
	result := make([]Node, len(acc))
	for i, m := range acc {
		result[i] = m
	}
	return result
}

func mergeTimeOr(a, b *TimeNode) *TimeNode {
	if slices.EqualFunc(a.Days, b.Days, daySpecEqual) {
		times := make([]TimeSpec, 0, len(a.Times)+len(b.Times))
		times = append(times, a.Times...)
		times = append(times, b.Times...)
		return &TimeNode{Pos: a.Pos, Days: a.Days, Times: times}
	}
	if slices.EqualFunc(a.Times, b.Times, timeSpecEqual) {
		days := make([]DaySpec, 0, len(a.Days)+len(b.Days))
		days = append(days, a.Days...)
		days = append(days, b.Days...)
		return &TimeNode{Pos: a.Pos, Days: days, Times: a.Times}
	}
	return nil
}

func daySpecEqual(a, b DaySpec) bool {
	switch a := a.(type) {
	case WeekdayLit:
		if b, ok := b.(WeekdayLit); ok {
			a.Pos = Pos{}
			b.Pos = Pos{}
			return a == b
		}
	case DateLit:
		if b, ok := b.(DateLit); ok {
			a.Pos = Pos{}
			b.Pos = Pos{}
			return a == b
		}
	}
	return false
}

func timeSpecEqual(a, b TimeSpec) bool {
	switch a := a.(type) {
	case TimeLit:
		if b, ok := b.(TimeLit); ok {
			return timeLitEqual(a, b)
		}
	case TimeRangeLit:
		if b, ok := b.(TimeRangeLit); ok {
			return timeLitEqual(a.Start, b.Start) && timeLitEqual(a.End, b.End)
		}
	}
	return false
}

func timeLitEqual(a, b TimeLit) bool {
	a.Pos = Pos{}
	b.Pos = Pos{}
	return a == b
}
