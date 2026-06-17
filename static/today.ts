'use strict'
export {}

// The "what's on" feed. Sessions for each day are server-rendered (so the page
// works without JS and for crawlers); this script turns the days into tabs and
// adds the live "now" line, the filter pills, the per-row hide action, and the
// shareable URL state.
//
// The shared starred facility store (starred.ts) is a global; see
// ottrecstarred.d.ts.

function normalizeText(s: string): string {
	return s.normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase()
}

interface DataJSON {
	updated: string
	weekdays: string[]
	categories: string[]
	periods: {label: string; start: number; end: number}[]
	sectors: string[]
	facilities: {slug: string; name: string; sector: string}[]
}

const data: DataJSON = JSON.parse(document.getElementById('today-data')!.textContent!)

// a server-rendered session row and its decoded filter attributes
interface Session {
	el: HTMLElement
	slug: string
	cats: number
	start: number
	end: number
}

// a server-rendered day section, surfaced as a tab
interface Day {
	el: HTMLElement
	date: string // ISO yyyy-mm-dd
	wd: number   // 0 = Sunday
	weekday: string
	month: string
	today: boolean
	tab: HTMLButtonElement
	sessions: Session[]
}

// advanced (ottrecql) search mode is server-side: the feed is already filtered,
// so this script skips the client-side filter pills, chips, exclude buttons, and
// f-* URL state, but keeps the tabs, "now" line, stars, and warning modals.
const advanced = document.body.classList.contains('today-advanced')

const feedEl = document.getElementById('today-feed')!
const tabsEl = document.getElementById('today-tabs')!
const noResultsEl = document.getElementById('today-noresults')!

const days: Day[] = [...feedEl.querySelectorAll<HTMLElement>('.today-day')].map((el) => {
	const head = el.querySelector('.today-day-head')!
	const sessions: Session[] = [...el.querySelectorAll<HTMLElement>('.today-session')].map((s) => ({
		el: s,
		slug: s.dataset['slug'] || '',
		cats: Number(s.dataset['cats'] || 0),
		start: Number(s.dataset['start'] || 0),
		end: Number(s.dataset['end'] || 0),
	}))
	return {
		el,
		date: el.dataset['date'] || '',
		wd: Number(el.dataset['wd'] || 0),
		weekday: head.querySelector('.wd')?.textContent || '',
		month: head.querySelector('.date')?.textContent || '',
		today: head.querySelector('.rel')?.textContent === 'Today',
		tab: document.createElement('button'),
		sessions,
	}
})
const sessions = days.flatMap((d) => d.sessions)

// which category indices actually appear, so the pill doesn't list dead options
const presentCats = new Set<number>()
for (const s of sessions)
	for (let c = 0; c < data.categories.length; c++)
		if (s.cats & (1 << c)) presentCats.add(c)

// filter state. Facilities have two independent controls: an *include* set
// (the Facilities dropdown — empty means all) and an *exclude* set (the per-row
// hide action). Excludes always win, and survive facility-list changes without
// silently dropping anything. The day is chosen by the tabs, not a filter. Time
// is an arbitrary start/end window in minutes from midnight (either side
// optional).
const filter = {
	include: new Set<string>(),
	exclude: new Set<string>(),
	cats: new Set<number>(),
	timeFrom: null as number | null,
	timeTo: null as number | null,
	starredOnly: false,
}

let activeDate = (days.find((d) => d.today) || days[0])?.date || ''

// session matching (everything except the per-day weekday split, which the tabs
// handle)
function sessionVisible(s: Session): boolean {
	if (filter.exclude.has(s.slug)) return false
	if (filter.include.size && !filter.include.has(s.slug)) return false
	if (filter.starredOnly && !ottrecStarred.has(s.slug)) return false
	if (filter.cats.size) {
		let mask = 0
		for (const c of filter.cats) mask |= 1 << c
		if (!(s.cats & mask)) return false
	}
	if (filter.timeTo !== null && s.start >= filter.timeTo) return false
	if (filter.timeFrom !== null && s.end <= filter.timeFrom) return false
	return true
}

// minutes-from-midnight <-> "HH:MM" (24h, what <input type=time> uses)
function parseClock(v: string): number | null {
	const m = /^(\d{1,2}):(\d{2})$/.exec(v)
	if (!m) return null
	return Number(m[1]) * 60 + Number(m[2])
}
function formatClock(mins: number): string {
	return String(Math.floor(mins / 60)).padStart(2, '0') + ':' + String(mins % 60).padStart(2, '0')
}
function clockLabel(mins: number): string {
	const h = Math.floor(mins / 60) % 24, m = mins % 60
	const h12 = h % 12 === 0 ? 12 : h % 12
	const suf = h < 12 ? 'am' : 'pm'
	return m === 0 ? h12 + suf : h12 + ':' + String(m).padStart(2, '0') + suf
}

// tabs

for (const d of days) {
	d.tab.type = 'button'
	d.tab.className = 'today-tab' + (d.today ? ' today' : '')
	d.tab.setAttribute('role', 'tab')
	const wd = document.createElement('span')
	wd.className = 'tab-wd'
	wd.textContent = d.today ? 'Today' : d.weekday
	const date = document.createElement('span')
	date.className = 'tab-date'
	date.textContent = d.month
	d.tab.append(wd, date)
	d.tab.addEventListener('click', () => {
		activeDate = d.date
		apply()
	})
	tabsEl.append(d.tab)
}

// main apply: show the active day, filter its sessions, and refresh everything
// that depends on it.
function apply() {
	if (!days.some((d) => d.date === activeDate))
		activeDate = (days.find((d) => d.today) || days[0])?.date || ''

	let shownCount = 0
	for (const d of days) {
		const isActive = d.date === activeDate
		d.el.hidden = !isActive
		d.tab.classList.toggle('active', isActive)
		if (isActive) {
			for (const s of d.sessions) {
				const vis = sessionVisible(s)
				s.el.hidden = !vis
				if (vis) shownCount++
			}
			// hide hour groups left with no visible sessions
			for (const hr of d.el.querySelectorAll<HTMLElement>('.today-hour'))
				hr.hidden = ![...hr.querySelectorAll<HTMLElement>('.today-session')].some((el) => !el.hidden)
		}
	}
	noResultsEl.hidden = advanced || shownCount > 0
	markNow()
	if (!advanced) {
		updateExcludeButtons()
		renderChips()
		for (const p of pills) p.updateButton()
		writeURL()
	}
}

// "now" line, in Ottawa time. Only the active day is shown, so this just tags
// today's visible sessions when today is the active day.

const TZ = 'America/Toronto'

function ottawaNowMinutes(): number {
	const parts = new Intl.DateTimeFormat('en-GB', {
		timeZone: TZ, hour: '2-digit', minute: '2-digit', hour12: false,
	}).formatToParts(new Date())
	const get = (t: string) => Number(parts.find((p) => p.type === t)?.value || 0)
	return (get('hour') % 24) * 60 + get('minute')
}

function markNow() {
	for (const el of feedEl.querySelectorAll('.now, .past')) el.classList.remove('now', 'past')

	const day = days.find((d) => d.date === activeDate)
	if (!day || !day.today || day.el.hidden) return

	const mins = ottawaNowMinutes()
	for (const s of day.sessions) {
		if (s.el.hidden) continue
		if (s.end <= mins) s.el.classList.add('past')
		else if (s.start <= mins) s.el.classList.add('now')
	}
}

// per-row "hide this facility" action

function updateExcludeButtons() {
	for (const btn of feedEl.querySelectorAll<HTMLButtonElement>('.t-exclude')) btn.hidden = false
}
feedEl.addEventListener('click', (ev) => {
	const target = ev.target as HTMLElement
	const exclude = target.closest<HTMLButtonElement>('.t-exclude')
	if (exclude) {
		ev.preventDefault()
		const slug = exclude.dataset['exclude']
		if (slug) {
			filter.exclude.add(slug)
			facilityPill?.syncChecks()
			apply()
		}
		return
	}
	const full = target.closest<HTMLButtonElement>('.t-fullsched')
	if (full) {
		ev.preventDefault()
		openScheduleModal(full)
		return
	}
	const warn = target.closest<HTMLButtonElement>('.t-warn[data-warn]')
	if (warn) {
		ev.preventDefault()
		openWarnModal(warn)
	}
})

// filter pills

const filtersEl = document.getElementById('today-filters')!
const chipsEl = document.getElementById('today-chips')!

interface PillOption {
	value: string
	label: string
	group?: string
}

interface Pill {
	updateButton: () => void
	syncChecks: () => void
}

const pills: Pill[] = []

// scaffolding shared by every pill: a button + a dropdown panel that opens on
// click and closes on outside-click/Escape.
function makePill(title: string): {pill: HTMLElement; button: HTMLButtonElement; panel: HTMLElement; search?: HTMLInputElement; setSearchable: () => HTMLInputElement; head: HTMLElement; list: HTMLElement} {
	const pill = document.createElement('div')
	pill.className = 'today-pill'
	const button = document.createElement('button')
	button.type = 'button'
	const panel = document.createElement('div')
	panel.className = 'today-pill-panel'
	panel.hidden = true
	pill.append(button, panel)
	filtersEl.append(pill)

	const head = document.createElement('div')
	head.className = 'panel-head'
	panel.append(head)
	const list = document.createElement('div')
	list.className = 'panel-list'
	panel.append(list)

	let search: HTMLInputElement | undefined
	const setSearchable = () => {
		search = document.createElement('input')
		search.type = 'search'
		search.placeholder = 'Search ' + title.toLowerCase()
		head.prepend(search)
		return search
	}

	button.addEventListener('click', (ev) => {
		ev.stopPropagation()
		const open = panel.hidden
		closeAllPanels()
		panel.hidden = !open
		if (open && search) search.focus()
	})
	panel.addEventListener('click', (ev) => ev.stopPropagation())
	return {pill, button, panel, get search() { return search }, setSearchable, head, list} as never
}

function closeAllPanels() {
	for (const p of filtersEl.querySelectorAll<HTMLElement>('.today-pill-panel')) p.hidden = true
}
document.addEventListener('click', closeAllPanels)
document.addEventListener('keydown', (ev) => {
	if (ev.key === 'Escape') closeAllPanels()
})

// a multi-select checkbox pill over a Set. `excludeSet`, if given, marks options
// in that set as struck-out with an "excluded" badge (the per-row hide action
// feeds it); `alsoActive` lets the pill light up for state beyond `selected`.
function buildCheckPill<T extends string | number>(cfg: {
	title: string
	options: PillOption[]
	parse: (v: string) => T
	selected: Set<T>
	searchable?: boolean
	note?: string
	excludeSet?: Set<T>
	alsoActive?: () => boolean
}): Pill {
	const {pill, button, head, list, setSearchable} = makePill(cfg.title)
	const search = cfg.searchable ? setSearchable() : undefined

	if (cfg.note) {
		const note = document.createElement('div')
		note.className = 'panel-note'
		note.textContent = cfg.note
		head.before(note)
	}

	const clearBtn = document.createElement('button')
	clearBtn.type = 'button'
	clearBtn.textContent = 'Clear'
	head.append(clearBtn)

	let hint: HTMLElement | undefined
	if (search) {
		hint = document.createElement('div')
		hint.className = 'panel-hint'
		list.after(hint)
	}

	const checks: {opt: PillOption; row: HTMLElement; input: HTMLInputElement; badge: HTMLButtonElement | undefined}[] = []
	let lastGroup = ''
	for (const opt of cfg.options) {
		if (opt.group && opt.group !== lastGroup) {
			lastGroup = opt.group
			const h = document.createElement('div')
			h.className = 'panel-group'
			h.textContent = opt.group
			list.append(h)
		}
		const row = document.createElement('label')
		row.className = 'check'
		const input = document.createElement('input')
		input.type = 'checkbox'
		const name = document.createElement('span')
		name.textContent = opt.label
		row.append(input, name)
		let badge: HTMLButtonElement | undefined
		if (cfg.excludeSet) {
			badge = document.createElement('button')
			badge.type = 'button'
			badge.className = 'ex-badge'
			badge.textContent = 'excluded'
			badge.title = 'Stop hiding this facility'
			badge.hidden = true
			badge.addEventListener('click', (ev) => {
				ev.preventDefault()
				cfg.excludeSet!.delete(cfg.parse(opt.value))
				syncChecks()
				apply()
			})
			row.append(badge)
		}
		list.append(row)
		input.addEventListener('change', () => {
			const v = cfg.parse(opt.value)
			if (input.checked) cfg.selected.add(v)
			else cfg.selected.delete(v)
			apply()
		})
		checks.push({opt, row, input, badge})
	}

	function updateButton() {
		button.replaceChildren(document.createTextNode(cfg.title))
		if (cfg.selected.size) {
			const c = document.createElement('span')
			c.className = 'pill-count'
			c.textContent = String(cfg.selected.size)
			button.append(c)
		}
		pill.classList.toggle('active', cfg.selected.size > 0 || !!cfg.alsoActive?.())
	}
	function syncChecks() {
		for (const {opt, row, input, badge} of checks) {
			input.checked = cfg.selected.has(cfg.parse(opt.value))
			if (cfg.excludeSet && badge) {
				const ex = cfg.excludeSet.has(cfg.parse(opt.value))
				row.classList.toggle('excluded', ex)
				badge.hidden = !ex
			}
		}
		updateButton()
	}

	clearBtn.addEventListener('click', () => {
		cfg.selected.clear()
		if (search && search.value) {
			search.value = ''
			runSearch()
		}
		syncChecks()
		apply()
	})

	function runSearch() {
		const q = normalizeText((search!.value || '').trim())
		let shownGroup: HTMLElement | null = null
		let groupHasVisible = false
		let shown = 0
		const flush = () => { if (shownGroup) shownGroup.hidden = !groupHasVisible }
		for (const el of list.children) {
			const h = el as HTMLElement
			if (h.classList.contains('panel-group')) {
				flush()
				shownGroup = h
				groupHasVisible = false
				continue
			}
			const label = h.querySelector('span')?.textContent || ''
			const vis = !q || normalizeText(label).includes(q)
			h.hidden = !vis
			if (vis) { groupHasVisible = true; shown++ }
		}
		flush()
		// make it clear whether the list is the full set or search-narrowed
		if (hint) hint.textContent = q
			? `Showing ${shown} of ${checks.length} facilities`
			: `All ${checks.length} facilities`
	}
	if (search) {
		search.addEventListener('input', runSearch)
		runSearch()
	}

	updateButton()
	const p: Pill = {updateButton, syncChecks}
	pills.push(p)
	return p
}

// build the pills (skipped entirely in advanced mode, where the feed is already
// filtered server-side and the query box replaces the pills)

let facilityPill: Pill | undefined
if (!advanced) {
	const facilityOptions: PillOption[] = []
	for (const sector of data.sectors)
		for (const f of data.facilities)
			if (f.sector === sector)
				facilityOptions.push({value: f.slug, label: f.name, group: sector})
	facilityPill = buildCheckPill({
		title: 'Facilities',
		options: facilityOptions,
		parse: (v) => v,
		selected: filter.include,
		searchable: true,
		note: 'Only facilities with sessions over the next week are listed. Check some to show only those; hidden ones (× in the feed) are struck out.',
		excludeSet: filter.exclude,
		alsoActive: () => filter.exclude.size > 0,
	})

	const catOptions: PillOption[] = data.categories
		.map((label, i) => ({label, i}))
		.filter(({i}) => presentCats.has(i))
		.map(({label, i}) => ({value: String(i), label}))
	buildCheckPill({title: 'Activity', options: catOptions, parse: (v) => Number(v), selected: filter.cats})
}

// separate start/end time picker pills, each a single time input

function buildTimeBoundPill(title: string, key: 'timeFrom' | 'timeTo'): Pill {
	const {pill, button, list, head} = makePill(title)
	head.remove() // the picker has its own clear button
	const row = document.createElement('div')
	row.className = 'time-picker'
	const input = document.createElement('input')
	input.type = 'time'
	const clear = document.createElement('button')
	clear.type = 'button'
	clear.className = 'time-clear'
	clear.textContent = 'Clear'
	row.append(input, clear)
	list.append(row)

	input.addEventListener('change', () => {
		filter[key] = parseClock(input.value)
		updateButton()
		apply()
	})
	clear.addEventListener('click', () => {
		filter[key] = null
		input.value = ''
		updateButton()
		apply()
	})

	function updateButton() {
		button.replaceChildren(document.createTextNode(title))
		const v = filter[key]
		if (v !== null) {
			const c = document.createElement('span')
			c.className = 'pill-count time'
			c.textContent = clockLabel(v)
			button.append(c)
		}
		pill.classList.toggle('active', filter[key] !== null)
	}
	function syncChecks() {
		input.value = filter[key] !== null ? formatClock(filter[key]!) : ''
		updateButton()
	}
	updateButton()
	const p: Pill = {updateButton, syncChecks}
	pills.push(p)
	return p
}
if (!advanced) {
	buildTimeBoundPill('Start', 'timeFrom')
	buildTimeBoundPill('End', 'timeTo')
}

// starred-only toggle pill, shown only when there are starred facilities

const starredPill = document.createElement('button')
starredPill.type = 'button'
starredPill.className = 'today-pill toggle'
starredPill.textContent = '★ Starred only'
starredPill.hidden = true
filtersEl.prepend(starredPill) // first in the row
starredPill.addEventListener('click', () => {
	filter.starredOnly = !filter.starredOnly
	apply()
})
function syncStarredPill() {
	const any = ottrecStarred.count() > 0
	if (!any) filter.starredOnly = false
	starredPill.hidden = !any
	starredPill.classList.toggle('active', filter.starredOnly)
}

// active-filter chips

function renderChips() {
	const chips: {label: string; clear: () => void}[] = []
	const facName = (slug: string) => data.facilities.find((f) => f.slug === slug)?.name || slug
	if (filter.starredOnly) chips.push({label: '★ Starred only', clear: () => filter.starredOnly = false})
	for (const slug of filter.include) chips.push({label: 'Only ' + facName(slug), clear: () => filter.include.delete(slug)})
	for (const slug of filter.exclude) chips.push({label: 'Hide ' + facName(slug), clear: () => filter.exclude.delete(slug)})
	for (const c of filter.cats) chips.push({label: data.categories[c]!, clear: () => filter.cats.delete(c)})
	if (filter.timeFrom !== null) chips.push({label: 'from ' + clockLabel(filter.timeFrom), clear: () => filter.timeFrom = null})
	if (filter.timeTo !== null) chips.push({label: 'until ' + clockLabel(filter.timeTo), clear: () => filter.timeTo = null})

	chipsEl.replaceChildren()
	for (const c of chips) {
		const btn = document.createElement('button')
		btn.type = 'button'
		btn.className = 'fchip'
		btn.textContent = c.label
		btn.addEventListener('click', () => {
			c.clear()
			syncAll()
			apply()
		})
		chipsEl.append(btn)
	}
	if (chips.length > 1) {
		const all = document.createElement('button')
		all.type = 'button'
		all.className = 'fchip clear-all'
		all.textContent = 'Clear all'
		all.addEventListener('click', () => {
			filter.include.clear()
			filter.exclude.clear()
			filter.cats.clear()
			filter.timeFrom = filter.timeTo = null
			filter.starredOnly = false
			syncAll()
			apply()
		})
		chipsEl.append(all)
	}
}

function syncAll() {
	for (const p of pills) p.syncChecks()
	syncStarredPill()
}

// URL state (f-* params, like the map filters), so links are shareable

function writeURL() {
	const params = new URLSearchParams()
	if (filter.include.size) params.set('f-fac', [...filter.include].join(','))
	if (filter.exclude.size) params.set('f-xfac', [...filter.exclude].join(','))
	if (filter.cats.size) params.set('f-cat', [...filter.cats].sort((a, b) => a - b).map((c) => data.categories[c]!).join(','))
	if (filter.timeFrom !== null) params.set('f-from', formatClock(filter.timeFrom))
	if (filter.timeTo !== null) params.set('f-to', formatClock(filter.timeTo))
	if (filter.starredOnly) params.set('f-starred', '1')
	if (![...params.keys()].length) {
		if (location.search) history.replaceState(null, '', location.pathname + location.hash)
		return
	}
	params.set('f-v', '1')
	params.set('f-t', data.updated)
	const qs = params.toString().replace(/%2C/gi, ',').replace(/%3A/gi, ':')
	const url = location.pathname + '?' + qs + location.hash
	if (url !== location.pathname + location.search + location.hash)
		history.replaceState(null, '', url)
}

function loadURL() {
	const params = new URLSearchParams(location.search)
	for (const slug of (params.get('f-fac') || '').split(',').filter(Boolean))
		if (data.facilities.some((f) => f.slug === slug)) filter.include.add(slug)
	for (const slug of (params.get('f-xfac') || '').split(',').filter(Boolean))
		filter.exclude.add(slug) // excludes survive even if the facility is gone (harmless)
	// categories: if any name is unknown (e.g. the data changed), drop them all
	// rather than applying a partial, misleading filter
	const catNames = (params.get('f-cat') || '').split(',').filter(Boolean)
	const catIdx = catNames.map((n) => data.categories.indexOf(n))
	if (!catIdx.includes(-1)) for (const i of catIdx) filter.cats.add(i)
	filter.timeFrom = parseClock(params.get('f-from') || '')
	filter.timeTo = parseClock(params.get('f-to') || '')
	filter.starredOnly = params.get('f-starred') === '1'
}

// warning modal: fetches the changes / special-date schedule fragment and shows
// it in a <dialog>, with the official-page link already at the bottom of the
// fragment.

const modal = document.createElement('dialog')
modal.className = 'today-modal'
modal.innerHTML =
	'<button type="button" class="today-modal-close" aria-label="Close">×</button>' +
	'<div class="today-modal-content"></div>'
document.body.append(modal)
const modalContent = modal.querySelector<HTMLElement>('.today-modal-content')!
const modalClose = modal.querySelector<HTMLButtonElement>('.today-modal-close')!
modalClose.addEventListener('click', () => modal.close())
modal.addEventListener('click', (ev) => {
	if (ev.target === modal) modal.close() // backdrop click
})

let modalToken = 0
async function loadModal(url: string, fallbackSource?: string) {
	const token = ++modalToken
	modalContent.innerHTML = '<p class="today-modal-loading">Loading…</p>'
	if (!modal.open) modal.showModal()
	try {
		const resp = await fetch(url)
		if (!resp.ok) throw new Error('status ' + resp.status)
		const html = await resp.text()
		if (token !== modalToken) return
		modalContent.innerHTML = html
	} catch {
		if (token !== modalToken) return
		modalContent.innerHTML = '<p class="today-modal-empty">Couldn’t load the details.</p>' +
			(fallbackSource ? '<p class="today-modal-source"><a href="' + fallbackSource + '" target="_blank" rel="noopener">View on the City of Ottawa website</a></p>' : '')
	}
}

function openWarnModal(btn: HTMLElement) {
	const slug = encodeURIComponent(btn.dataset['slug'] || '')
	const group = encodeURIComponent(btn.dataset['group'] || '0')
	const kind = btn.dataset['warn']
	const url = kind === 'errors'
		? '/api/errors?facility=' + slug
		: kind === 'reservations'
			? '/api/reservations?facility=' + slug + '&group=' + group
			: '/api/changes?facility=' + slug + '&group=' + group
	loadModal(url, btn.dataset['source'])
}

// the full-schedule modal reuses the map's facility fragment, scoped to the
// session's schedule group via ?group.
function openScheduleModal(btn: HTMLElement) {
	const slug = encodeURIComponent(btn.dataset['slug'] || '')
	const group = encodeURIComponent(btn.dataset['group'] || '0')
	loadModal('/map/facility/' + slug + '?group=' + group, btn.dataset['source'])
}

// turn the server-rendered "updated at X" timestamp into a relative one

function relativeUpdated() {
	const el = document.getElementById('today-updated-time') as HTMLTimeElement | null
	if (!el || !el.dateTime) return
	const then = new Date(el.dateTime).getTime()
	if (isNaN(then)) return
	const mins = Math.round((Date.now() - then) / 60000)
	let rel: string
	if (mins < 1) rel = 'just now'
	else if (mins < 60) rel = mins + (mins === 1 ? ' minute ago' : ' minutes ago')
	else if (mins < 60 * 24) {
		const h = Math.round(mins / 60)
		rel = h + (h === 1 ? ' hour ago' : ' hours ago')
	} else {
		const d = Math.round(mins / (60 * 24))
		rel = d + (d === 1 ? ' day ago' : ' days ago')
	}
	el.textContent = rel
	el.title = new Date(el.dateTime).toLocaleString()
}

// init

if (!advanced) {
	loadURL()
	syncAll()
}
ottrecStarred.onchange(() => {
	syncStarredPill()
	if (filter.starredOnly) apply()
})
if (!advanced) filtersEl.hidden = false
tabsEl.hidden = false
apply()
relativeUpdated()
setInterval(markNow, 60_000)
setInterval(relativeUpdated, 60_000)
