'use strict';
(function () {

// error banner shown at the bottom of the page if a js error occurs

function showError(msg) {
	let banner = document.getElementById('js-error-banner');
	if (!banner) {
		banner = document.createElement('div');
		banner.id = 'js-error-banner';
		const text = document.createElement('span');
		const close = document.createElement('button');
		close.type = 'button';
		close.textContent = '×';
		close.title = 'Dismiss';
		close.addEventListener('click', () => banner.remove());
		banner.append(text, close);
		document.body.append(banner);
	}
	banner.firstChild.textContent = 'Something went wrong: ' + msg;
}
window.addEventListener('error', (ev) => showError(ev.message || 'unknown error'));
window.addEventListener('unhandledrejection', (ev) => showError((ev.reason && ev.reason.message) || String(ev.reason)));

/**
 * FacilityData wraps the JSON data island embedded in the page and answers
 * queries about facilities, activities, and availability.
 *
 * A filter is a plain object:
 *   days       Set of enabled weekday indices (0 = Sunday); empty = all
 *   slots      Set of enabled time slot indices (into .slots); empty = all
 *   categories Set of enabled category indices (into .categories); empty = all
 *   activities Set of enabled activity indices (into .activities); empty = all
 *   name       substring to match against the facility name, case-insensitive
 */
class FacilityData {
	// per-(facility, activity) availability entries
	#entryActivity; // Uint16Array: activity index of entry e
	#entryMask;     // Uint8Array: 7 bytes per entry; byte d bit s = available on weekday d during slot s
	#entryStart;    // Uint32Array: facility i owns entries entryStart[i] ... entryStart[i+1]-1
	#activityCats;  // Uint16Array: category bitmask per activity
	#nameLower;     // lowercased facility names for matching

	constructor(json) {
		this.updated = json.updated;
		this.days = json.days;
		this.slots = json.slots;
		this.categories = json.categories;
		this.activities = json.activities;
		this.#activityCats = Uint16Array.from(json.activityCategories);
		this.facilities = json.facilities.map((f, i) => ({
			index: i,
			slug: f.slug,
			name: f.name,
			address: f.address || '',
			lat: f.lat || 0,
			lng: f.lng || 0,
		}));
		this.#nameLower = this.facilities.map((f) => f.name.toLowerCase());

		const packed = json.facilities.map((f) => atob(f.mask || ''));
		const total = packed.reduce((n, p) => n + p.length / 9, 0);
		this.#entryActivity = new Uint16Array(total);
		this.#entryMask = new Uint8Array(total * 7);
		this.#entryStart = new Uint32Array(packed.length + 1);
		let e = 0;
		packed.forEach((p, i) => {
			this.#entryStart[i] = e;
			for (let o = 0; o + 9 <= p.length; o += 9, e++) {
				this.#entryActivity[e] = p.charCodeAt(o) | (p.charCodeAt(o + 1) << 8);
				for (let k = 0; k < 7; k++)
					this.#entryMask[e * 7 + k] = p.charCodeAt(o + 2 + k);
			}
		});
		this.#entryStart[packed.length] = e;
	}

	// #prepare converts a filter into the internal selection representation.
	#prepare(filter) {
		const slotBits = filter.slots.size
			? [...filter.slots].reduce((m, s) => m | (1 << s), 0)
			: (1 << this.slots.length) - 1;
		const days = filter.days.size ? filter.days : new Set(this.days.map((_, i) => i));
		const mask = new Uint8Array(7);
		for (const d of days) mask[d] = slotBits;
		return {
			name: filter.name.trim().toLowerCase(),
			activities: filter.activities.size ? filter.activities : null,
			cats: [...filter.categories].reduce((m, c) => m | (1 << c), 0),
			days,
			mask,
			timeFiltered: filter.slots.size > 0 || filter.days.size > 0,
		};
	}

	// #activityAllowed reports whether activity a passes the activity and
	// category parts of the filter.
	#activityAllowed(a, q) {
		if (q.activities && !q.activities.has(a)) return false;
		if (q.cats && !(this.#activityCats[a] & q.cats)) return false;
		return true;
	}

	#entryTimeMatches(e, q) {
		let any = false;
		for (let k = 0; k < 7; k++) {
			const m = this.#entryMask[e * 7 + k];
			if (m & q.mask[k]) return true;
			if (m) any = true;
		}
		// entries with no parsed times match as long as no time filter is active
		return !any && !q.timeFiltered;
	}

	#facilityMatches(i, q) {
		if (q.name && !this.#nameLower[i].includes(q.name)) return false;
		const start = this.#entryStart[i], end = this.#entryStart[i + 1];
		if (start === end) // no activity data at all; show unless filtering by activity, category, or time
			return !q.activities && !q.cats && !q.timeFiltered;
		for (let e = start; e < end; e++) {
			if (!this.#activityAllowed(this.#entryActivity[e], q)) continue;
			if (this.#entryTimeMatches(e, q)) return true;
		}
		return false;
	}

	// matchingFacilities returns the indices of facilities matching the filter.
	matchingFacilities(filter) {
		const q = this.#prepare(filter);
		const out = [];
		for (let i = 0; i < this.facilities.length; i++)
			if (this.#facilityMatches(i, q)) out.push(i);
		return out;
	}

	// activityInCategories reports whether activity a is in any of the given
	// categories (an empty set matches all).
	activityInCategories(a, categories) {
		if (!categories.size) return true;
		for (const c of categories)
			if (this.#activityCats[a] & (1 << c)) return true;
		return false;
	}

	// facilityActivities returns the sorted activity indices offered by a facility.
	facilityActivities(i) {
		const out = [];
		for (let e = this.#entryStart[i]; e < this.#entryStart[i + 1]; e++)
			out.push(this.#entryActivity[e]);
		return out;
	}

	// activityCounts returns, for each activity, the number of facilities which
	// would match the filter if only that activity were selected (i.e., the
	// activity part of the filter itself is ignored, but the categories are
	// still applied).
	activityCounts(filter) {
		const q = this.#prepare(filter);
		const counts = new Uint32Array(this.activities.length);
		for (let i = 0; i < this.facilities.length; i++) {
			if (q.name && !this.#nameLower[i].includes(q.name)) continue;
			for (let e = this.#entryStart[i]; e < this.#entryStart[i + 1]; e++) {
				const a = this.#entryActivity[e];
				if (q.cats && !(this.#activityCats[a] & q.cats)) continue;
				if (this.#entryTimeMatches(e, q)) counts[a]++;
			}
		}
		return counts;
	}

	// categoryCounts returns, for each category, the number of facilities which
	// would match the filter if only that category were selected (i.e., the
	// category and activity parts of the filter are ignored).
	categoryCounts(filter) {
		const q = this.#prepare(filter);
		const counts = new Uint32Array(this.categories.length);
		for (let i = 0; i < this.facilities.length; i++) {
			if (q.name && !this.#nameLower[i].includes(q.name)) continue;
			let bits = 0;
			for (let e = this.#entryStart[i]; e < this.#entryStart[i + 1]; e++)
				if (this.#entryTimeMatches(e, q))
					bits |= this.#activityCats[this.#entryActivity[e]];
			for (let c = 0; c < this.categories.length; c++)
				if (bits & (1 << c)) counts[c]++;
		}
		return counts;
	}

	// slotCounts returns, for each time slot, the number of facilities which
	// would match the filter if only that slot were selected (i.e., the slot
	// part of the filter itself is ignored).
	slotCounts(filter) {
		const q = this.#prepare(filter);
		const counts = new Uint32Array(this.slots.length);
		for (let i = 0; i < this.facilities.length; i++) {
			if (q.name && !this.#nameLower[i].includes(q.name)) continue;
			let slotBits = 0;
			for (let e = this.#entryStart[i]; e < this.#entryStart[i + 1]; e++) {
				if (!this.#activityAllowed(this.#entryActivity[e], q)) continue;
				for (const d of q.days) slotBits |= this.#entryMask[e * 7 + d];
			}
			for (let s = 0; s < this.slots.length; s++)
				if (slotBits & (1 << s)) counts[s]++;
		}
		return counts;
	}
}

const data = new FacilityData(JSON.parse(document.getElementById('map-data').textContent));

const filter = {
	days: new Set(),
	slots: new Set(),
	categories: new Set(),
	activities: new Set(),
	name: '',
};
let order = 'alpha';
let visible = [];

const listEl = document.getElementById('fac-list');
const searchEl = document.getElementById('fac-search');
const mobileQuery = window.matchMedia('(max-width: 900px)');
const darkQuery = window.matchMedia('(prefers-color-scheme: dark)');
const facCountEl = document.getElementById('fac-count');
const sheetToggleEl = document.getElementById('fac-sheet-toggle');
const filterChipsEl = document.getElementById('filter-chips');

// the chips live in the mobile filter bar on narrow screens and overlaid on
// the map on wide ones
function placeChips() {
	if (mobileQuery.matches) document.querySelector('.map-filterbar').append(filterChipsEl);
	else document.getElementById('map-chips').append(filterChipsEl);
}
mobileQuery.addEventListener('change', placeChips);
placeChips();
const activitiesFilteredEl = document.getElementById('filter-activities-filtered');

// map

const map = L.map('map').setView([45.4215, -75.6972], 11);

// light/dark tiles following the color scheme
const tileURL = (dark) => 'https://{s}.basemaps.cartocdn.com/' + (dark ? 'dark_all' : 'light_all') + '/{z}/{x}/{y}{r}.png';
const tiles = L.tileLayer(tileURL(darkQuery.matches), {
	subdomains: 'abcd',
	maxZoom: 20,
	attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors &copy; <a href="https://carto.com/attributions">CARTO</a> &copy; <a href="https://www.pgaskin.net">Patrick Gaskin</a>',
}).addTo(map);
darkQuery.addEventListener('change', (ev) => tiles.setUrl(tileURL(ev.matches)));

const markers = new Map(); // facility index -> L.Marker
const popupCache = new Map(); // slug -> Promise<string>
for (const f of data.facilities) {
	if (!f.lat && !f.lng) continue;
	const marker = L.marker([f.lat, f.lng], {
		icon: L.divIcon({
			className: 'fac-pin-wrap',
			html: '<div class="fac-pin"></div>',
			iconSize: [30, 30],
			iconAnchor: [15, 15],
		}),
	});
	marker.bindTooltip(f.name, {direction: 'top', offset: [0, -12]});
	marker.bindPopup('<div class="fac-popup-loading">Loading…</div>', {
		minWidth: 400,
		maxWidth: 600,
		maxHeight: 520,
	});
	marker.on('popupopen', (ev) => {
		if (mobileQuery.matches) {
			// on mobile, the details are shown in a panel over the map instead
			// of an anchored popup, so they can't get cut off
			map.closePopup(ev.popup);
			openDetail(f);
		} else {
			// let the popup grow to fit the schedule tables if there's room
			const size = map.getSize();
			ev.popup.options.maxWidth = Math.max(400, Math.min(880, size.x - 120));
			ev.popup.options.maxHeight = Math.max(320, Math.min(680, size.y - 120));
			loadPopup(f, ev.popup);
		}
	});
	marker.on('mouseover', () => setHighlight(f.index, true));
	marker.on('mouseout', () => setHighlight(f.index, false));
	markers.set(f.index, marker);
}

// fetchFacility fetches (and caches) the facility popup content.
function fetchFacility(f) {
	if (!popupCache.has(f.slug))
		popupCache.set(f.slug, fetch('/map/facility/' + encodeURIComponent(f.slug)).then((resp) => {
			if (!resp.ok) throw new Error('status ' + resp.status);
			return resp.text();
		}));
	return popupCache.get(f.slug);
}

async function loadPopup(f, popup) {
	try {
		popup.setContent(await fetchFacility(f));
	} catch (err) {
		popupCache.delete(f.slug);
		popup.setContent('<div class="fac-popup-error">Failed to load facility info.</div>');
	}
}

// facility details panel over the map for mobile

const detailContentEl = document.getElementById('fac-detail-content');
let detailToken = 0;

async function openDetail(f) {
	const token = ++detailToken;
	detailContentEl.innerHTML = '<div class="fac-popup-loading">Loading…</div>';
	document.body.classList.add('detail-open');
	let html;
	try {
		html = await fetchFacility(f);
	} catch (err) {
		popupCache.delete(f.slug);
		html = '<div class="fac-popup-error">Failed to load facility info.</div>';
	}
	if (token === detailToken) detailContentEl.innerHTML = html;
}

function setHighlight(i, on) {
	const marker = markers.get(i);
	if (marker) {
		const el = marker.getElement();
		if (el) el.classList.toggle('hl', on);
		marker.setZIndexOffset(on ? 1000 : 0);
	}
	const item = listEl.querySelector('li[data-index="' + i + '"]');
	if (item) {
		item.classList.toggle('hl', on);
		if (on) item.scrollIntoView({block: 'nearest'});
	}
}

function focusFacility(i) {
	const f = data.facilities[i];
	const marker = markers.get(i);
	if (!marker) return;
	document.body.classList.remove('list-open');
	if (!map.getBounds().contains([f.lat, f.lng]))
		map.setView([f.lat, f.lng], Math.max(map.getZoom(), 14));
	marker.openPopup();
}

// filter controls

let dayBtns = [], slotRows = [], catRows = [], actRows = [];

function buildFilters() {
	const daysEl = document.getElementById('filter-days');
	dayBtns = data.days.map((label, d) => {
		const btn = document.createElement('button');
		btn.type = 'button';
		btn.textContent = label;
		btn.addEventListener('click', () => {
			if (filter.days.has(d)) filter.days.delete(d);
			else filter.days.add(d);
			syncControls();
			update();
		});
		daysEl.append(btn);
		return btn;
	});
	slotRows = buildCheckList(document.getElementById('filter-slots'), data.slots, filter.slots);
	catRows = buildCheckList(document.getElementById('filter-categories'), data.categories, filter.categories, applyCategorySelection);
	actRows = buildCheckList(document.getElementById('filter-activities'), data.activities, filter.activities);
}

// applyCategorySelection forces the activity selection to match the selected
// categories: activities in a selected category are checked, and the rest are
// unchecked. The user can still uncheck individual activities afterwards.
function applyCategorySelection() {
	filter.activities.clear();
	if (filter.categories.size)
		for (let a = 0; a < data.activities.length; a++)
			if (data.activityInCategories(a, filter.categories))
				filter.activities.add(a);
}

function buildCheckList(el, labels, set, changed) {
	return labels.map((label, i) => {
		const row = document.createElement('label');
		row.className = 'check';
		const input = document.createElement('input');
		input.type = 'checkbox';
		input.addEventListener('change', () => {
			if (input.checked) set.add(i);
			else set.delete(i);
			if (changed) changed();
			syncControls();
			update();
		});
		const name = document.createElement('span');
		name.className = 'name';
		name.textContent = label;
		const count = document.createElement('span');
		count.className = 'count';
		row.append(input, name, count);
		el.append(row);
		return row;
	});
}

function syncControls() {
	dayBtns.forEach((btn, d) => btn.classList.toggle('on', filter.days.has(d)));
	slotRows.forEach((row, i) => row.querySelector('input').checked = filter.slots.has(i));
	catRows.forEach((row, i) => row.querySelector('input').checked = filter.categories.has(i));
	actRows.forEach((row, i) => row.querySelector('input').checked = filter.activities.has(i));
	searchEl.value = filter.name;
}

function applyCounts(rows, counts) {
	rows.forEach((row, i) => {
		row.querySelector('.count').textContent = counts[i];
		row.classList.toggle('zero', counts[i] === 0);
	});
}

// syncActivityVisibility limits the visible activity filter options to the
// selected categories (still showing explicitly selected activities).
function syncActivityVisibility() {
	const catFiltered = filter.categories.size > 0;
	actRows.forEach((row, a) => {
		row.hidden = catFiltered && !data.activityInCategories(a, filter.categories) && !filter.activities.has(a);
	});
	activitiesFilteredEl.hidden = !catFiltered;
}

// rendering

function update() {
	visible = data.matchingFacilities(filter);
	sortVisible();
	renderList();
	updateMarkers();
	applyCounts(slotRows, data.slotCounts(filter));
	applyCounts(catRows, data.categoryCounts(filter));
	applyCounts(actRows, data.activityCounts(filter));
	syncActivityVisibility();
	renderChips();
	const count = visible.length + '/' + data.facilities.length + ' facilit' + (data.facilities.length === 1 ? 'y' : 'ies');
	facCountEl.textContent = count;
	sheetToggleEl.textContent = count + (document.body.classList.contains('list-open') ? ' ▾' : ' ▴');
}

function cmpName(a, b) {
	return data.facilities[a].name.localeCompare(data.facilities[b].name);
}

function sortVisible() {
	if (order === 'distance') {
		const c = map.getCenter();
		const kx = Math.cos(c.lat * Math.PI / 180);
		const dist = (i) => {
			const f = data.facilities[i];
			if (!f.lat && !f.lng) return Infinity;
			const dx = (f.lng - c.lng) * kx, dy = f.lat - c.lat;
			return dx * dx + dy * dy;
		};
		visible.sort((a, b) => dist(a) - dist(b) || cmpName(a, b));
	} else {
		visible.sort(cmpName);
	}
}

function renderList() {
	const maxChips = 8;
	const frag = document.createDocumentFragment();
	for (const i of visible) {
		const f = data.facilities[i];
		const li = document.createElement('li');
		li.dataset.index = i;
		li.tabIndex = 0;
		const h = document.createElement('h2');
		h.textContent = f.name;
		const addr = document.createElement('p');
		addr.className = 'addr';
		addr.textContent = f.address;
		const chips = document.createElement('p');
		chips.className = 'chips';
		const acts = data.facilityActivities(i);
		acts.sort((a, b) => (filter.activities.has(b) - filter.activities.has(a)) || a - b);
		for (const a of acts.slice(0, maxChips)) {
			const chip = document.createElement('span');
			chip.className = filter.activities.has(a) ? 'chip sel' : 'chip';
			chip.textContent = data.activities[a];
			chips.append(chip);
		}
		if (acts.length > maxChips) {
			const chip = document.createElement('span');
			chip.className = 'chip';
			chip.textContent = '+' + (acts.length - maxChips);
			chips.append(chip);
		}
		li.append(h, addr, chips);
		li.addEventListener('mouseenter', () => setHighlight(i, true));
		li.addEventListener('mouseleave', () => setHighlight(i, false));
		li.addEventListener('click', () => focusFacility(i));
		frag.append(li);
	}
	listEl.replaceChildren(frag);
}

function updateMarkers() {
	const visibleSet = new Set(visible);
	for (const [i, marker] of markers) {
		const want = visibleSet.has(i);
		if (want && !map.hasLayer(marker)) marker.addTo(map);
		else if (!want && map.hasLayer(marker)) marker.remove();
	}
}

function renderChips() {
	
	const chips = [];
	if (filter.days.size)
		chips.push({
			label: [...filter.days].sort((a, b) => a - b).map((d) => data.days[d]).join(', '),
			clear: () => filter.days.clear(),
		});
	for (const s of [...filter.slots].sort((a, b) => a - b))
		chips.push({label: data.slots[s], clear: () => filter.slots.delete(s)});
	for (const c of [...filter.categories].sort((x, y) => x - y))
		chips.push({label: data.categories[c], clear: () => {
			filter.categories.delete(c);
			applyCategorySelection();
		}});
	for (const a of [...filter.activities].sort((x, y) => x - y))
		if (!data.activityInCategories(a, filter.categories) || !filter.categories.size)
			chips.push({label: data.activities[a], clear: () => filter.activities.delete(a)});
	if (filter.name.trim())
		chips.push({label: '“' + filter.name.trim() + '”', clear: () => filter.name = ''});
	filterChipsEl.replaceChildren(...chips.map((c) => {
		const btn = document.createElement('button');
		btn.type = 'button';
		btn.className = 'fchip';
		btn.textContent = c.label;
		btn.addEventListener('click', () => {
			c.clear();
			syncControls();
			update();
		});
		return btn;
	}));
}

// wiring

searchEl.addEventListener('input', () => {
	filter.name = searchEl.value;
	update();
});
document.getElementById('fac-order').addEventListener('change', (ev) => {
	order = ev.target.value;
	update();
});
map.on('moveend', () => {
	if (order === 'distance') update();
});
document.getElementById('filter-days-all').addEventListener('click', () => {
	filter.days = new Set(data.days.map((_, i) => i));
	syncControls();
	update();
});
document.getElementById('filter-days-none').addEventListener('click', () => {
	filter.days.clear();
	syncControls();
	update();
});
document.getElementById('filter-slots-all').addEventListener('click', () => {
	filter.slots = new Set(data.slots.map((_, i) => i));
	syncControls();
	update();
});
document.getElementById('filter-slots-none').addEventListener('click', () => {
	filter.slots.clear();
	syncControls();
	update();
});
document.getElementById('btn-filters').addEventListener('click', () => document.body.classList.add('filters-open'));
document.getElementById('btn-filters-done').addEventListener('click', () => document.body.classList.remove('filters-open'));
document.getElementById('fac-detail-close').addEventListener('click', () => document.body.classList.remove('detail-open'));
activitiesFilteredEl.addEventListener('click', () => {
	// clear the category filters, but leave the checked activities alone
	filter.categories.clear();
	syncControls();
	update();
});
sheetToggleEl.addEventListener('click', () => {
	document.body.classList.toggle('list-open');
	update();
});

buildFilters();
syncControls();
update();

})();
