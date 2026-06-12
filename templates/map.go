package templates

import (
	"encoding/base64"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/pgaskin/ottrec-website/pkg/ottrecidx"
)

// mapDays contains the weekday labels for the map filter, starting at Sunday to
// match time.Weekday.
var mapDays = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

// mapSlots contains the time slot ranges for the map filter as inclusive start
// and exclusive end minutes from midnight.
var mapSlots = [][2]int{
	{0, 6 * 60},
	{6 * 60, 9 * 60},
	{9 * 60, 11 * 60},
	{11 * 60, 13 * 60},
	{13 * 60, 16 * 60},
	{16 * 60, 18 * 60},
	{18 * 60, 21 * 60},
	{21 * 60, 24 * 60},
}

func mapSlotLabel(slot [2]int) string {
	return fmt.Sprintf("%02d:%02d-%02d:%02d", slot[0]/60, slot[0]%60, (slot[1]/60)%24, slot[1]%60)
}

// mapCategories contains the predefined activity categories for the map
// filter, matched against the normalized activity names. An activity can be in
// multiple categories, and ones not matching any are put in [mapCategoryOther].
var mapCategories = []struct {
	Name  string
	Match *regexp.Regexp
}{
	{"Swimming", regexp.MustCompile(`swim`)},
	{"Lane Swim", regexp.MustCompile(`lane swim`)},
	{"Aquafit", regexp.MustCompile(`aqua`)},
	{"Skating", regexp.MustCompile(`skat`)},
	{"Hockey", regexp.MustCompile(`hockey|shinny|stick and puck|ringette`)},
	{"Badminton", regexp.MustCompile(`badminton`)},
	{"Basketball", regexp.MustCompile(`basketball`)},
	{"Volleyball", regexp.MustCompile(`volleyball`)},
	{"Pickleball", regexp.MustCompile(`pickleball`)},
}

const mapCategoryOther = "Other"

// mapDataJSON is embedded into the map page as a JSON data island and consumed
// by the FacilityData class in the inline script.
type mapDataJSON struct {
	Updated    string   `json:"updated"`
	Days       []string `json:"days"`
	Slots      []string `json:"slots"`
	Categories []string `json:"categories"`
	Activities []string `json:"activities"`
	// ActivityCategories contains a bitmask of indexes into Categories for
	// each activity in Activities.
	ActivityCategories []int             `json:"activityCategories"`
	Facilities         []mapFacilityJSON `json:"facilities"`
}

type mapFacilityJSON struct {
	Slug    string  `json:"slug"`
	Name    string  `json:"name"`
	Address string  `json:"address,omitempty"`
	Lat     float32 `json:"lat,omitempty"`
	Lng     float32 `json:"lng,omitempty"`
	// Mask is a base64-encoded sequence of 9-byte entries, one per activity
	// offered by the facility, consisting of the little-endian uint16 index
	// into Activities followed by 7 bytes where byte d bit s is set if the
	// activity is available on weekday d (Sunday=0) during time slot s. An
	// activity with times which could not be parsed during scraping may have
	// an all-zero mask.
	Mask string `json:"mask,omitempty"`
}

func mapActivityName(act ottrecidx.ActivityRef) string {
	if s := act.GetName(); s != "" {
		return s
	}
	return strings.ToLower(strings.Join(strings.Fields(act.GetLabel()), " "))
}

// maskSetRange sets the slot bits overlapping the clock range start-end (in
// minutes) on weekday wd, wrapping overnight ranges onto the next day.
func maskSetRange(m *[7]byte, slots [][2]int, wd, start, end int) {
	for start >= 24*60 {
		wd = (wd + 1) % 7
		start -= 24 * 60
		end -= 24 * 60
	}
	if end > 24*60 {
		maskSetRange(m, slots, (wd+1)%7, 0, min(end-24*60, 24*60))
		end = 24 * 60
	}
	for s, slot := range slots {
		if start < slot[1] && slot[0] < end {
			m[wd] |= 1 << s
		}
	}
}

func buildMapData(data ottrecidx.DataRef) mapDataJSON {
	// collect and sort the distinct activity names
	actIdx := map[string]int{}
	for act := range data.Activities() {
		if name := mapActivityName(act); name != "" {
			actIdx[name] = 0
		}
	}
	actNames := slices.Sorted(maps.Keys(actIdx))
	for i, name := range actNames {
		actIdx[name] = i
	}

	// categorize the activities
	catNames := make([]string, 0, len(mapCategories)+1)
	for _, cat := range mapCategories {
		catNames = append(catNames, cat.Name)
	}
	catNames = append(catNames, mapCategoryOther)
	actCats := make([]int, len(actNames))
	for i, name := range actNames {
		for c, cat := range mapCategories {
			if cat.Match.MatchString(name) {
				actCats[i] |= 1 << c
			}
		}
		if actCats[i] == 0 {
			actCats[i] = 1 << len(mapCategories)
		}
	}

	// pack the per-facility availability masks
	var facs []mapFacilityJSON
	slugs := map[string]bool{}
	for fac := range data.Facilities() {
		masks := map[int]*[7]byte{}
		for act := range fac.Activities() {
			name := mapActivityName(act)
			if name == "" {
				continue
			}
			m := masks[actIdx[name]]
			if m == nil {
				m = new([7]byte)
				masks[actIdx[name]] = m
			}
			for tm := range act.Times() {
				wd, ok := tm.GetWeekday()
				if !ok {
					continue
				}
				r, ok := tm.GetRange()
				if !ok {
					continue
				}
				maskSetRange(m, mapSlots, int(wd), int(r.Start), int(r.End))
			}
		}
		raw := make([]byte, 0, len(masks)*9)
		for _, ai := range slices.Sorted(maps.Keys(masks)) {
			raw = append(raw, byte(ai), byte(ai>>8))
			raw = append(raw, masks[ai][:]...)
		}
		var lat, lng float32
		if x, y, ok := fac.GetLngLat(); ok {
			lng, lat = x, y
		}
		facs = append(facs, mapFacilityJSON{
			Slug:    mapUniqueSlug(slugs, fac.GetName()),
			Name:    fac.GetName(),
			Address: mapOneLineAddress(fac.GetAddress()),
			Lat:     lat,
			Lng:     lng,
			Mask:    base64.StdEncoding.EncodeToString(raw),
		})
	}

	slotLabels := make([]string, len(mapSlots))
	for i, slot := range mapSlots {
		slotLabels[i] = mapSlotLabel(slot)
	}

	return mapDataJSON{
		Updated:            data.Index().Updated().In(ottrecidx.TZ).Format("2006-01-02"),
		Days:               mapDays,
		Slots:              slotLabels,
		Categories:         catNames,
		Activities:         actNames,
		ActivityCategories: actCats,
		Facilities:         facs,
	}
}

// MapFacilityBySlug resolves a slug from the map page back to a facility.
func MapFacilityBySlug(data ottrecidx.DataRef, slug string) (ottrecidx.FacilityRef, bool) {
	seen := map[string]bool{}
	for fac := range data.Facilities() {
		if mapUniqueSlug(seen, fac.GetName()) == slug {
			return fac, true
		}
	}
	return ottrecidx.FacilityRef{}, false
}

func mapSlug(name string) string {
	var b strings.Builder
	dash := true // no leading dash
	for _, r := range strings.ToLower(name) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			dash = false
		default:
			if !dash {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// mapUniqueSlug returns a slug for name which is unique within seen, which must
// be updated and matched against facilities in the same (i.e., document) order.
func mapUniqueSlug(seen map[string]bool, name string) string {
	slug := mapSlug(name)
	if slug == "" {
		slug = "facility"
	}
	s := slug
	for n := 2; seen[s]; n++ {
		s = slug + "-" + strconv.Itoa(n)
	}
	seen[s] = true
	return s
}

func mapOneLineAddress(addr string) string {
	var b strings.Builder
	for line := range strings.Lines(addr) {
		if line = strings.TrimSpace(line); line != "" {
			if b.Len() != 0 {
				b.WriteString(", ")
			}
			b.WriteString(line)
		}
	}
	return b.String()
}
