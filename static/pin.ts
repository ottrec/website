'use strict'
import * as L from 'leaflet'

// The facility map pin, shared by the full map (map.ts) and the activity
// page's minimal map (activitymap.ts): a thin teardrop anchored at its tip,
// styled per page (.fac-pin in map.css / activity.css).

// pinHTML is the marker markup; exported separately so map.ts can swap it in
// place when the starred state changes.
export const pinHTML = (starred: boolean) =>
	'<svg class="fac-pin' + (starred ? ' starred' : '') + '" viewBox="0 0 14 20" aria-hidden="true">' +
	'<path d="M7 19.2C6 16.2 2.2 11.9 2.2 6.8a4.8 4.8 0 1 1 9.6 0C11.8 11.9 8 16.2 7 19.2Z"/>' +
	'</svg>'

// pinIcon builds the marker icon. The wrapper is larger than the visible pin
// to give a bigger click target; the anchor is the teardrop's tip (bottom
// center), and the popup/tooltip anchors sit just above the head.
export function pinIcon(starred: boolean): L.DivIcon {
	return L.divIcon({
		className: 'fac-pin-wrap',
		html: pinHTML(starred),
		iconSize: [30, 30],
		iconAnchor: [15, 30],
		popupAnchor: [0, -26],
		tooltipAnchor: [0, -26],
	})
}
