package ottrecflt

// Query filters activity times.
type Query struct {
	// Include is list of filters to include if any of them match. If empty, all
	// are included.
	Include []Filter

	// Exclude is a list of filters to exclude after applying the includes.
	Exclude []Filter
}

// Filter is a set of AND'd facets, each of which can have multiple OR'd values.
type Filter struct {
	FacilityName []Facet[FacilityName]
}

// Facet is a generic facet.
type Facet[T FacetType] struct {
	Negate bool
	Params T
}

type FacetType interface {
	FacilityName
}

type FacilityName struct {
}
