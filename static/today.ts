'use strict'
import {normalizeText} from './text'

// The "what's on" feed. Sessions for each day are server-rendered (so the page
// works without JS and for crawlers); this script turns the days into tabs and
// adds the live "now" line, the filter pills, the per-row hide action, and the
// shareable URL state.
//
// The shared starred facility store (starred.ts) is a global; see
// ottrecstarred.d.ts.

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
	text: string     // normalized activity + facility names, for the quick filter
	compact: string  // text with the spaces dropped ("aqua fit" <-> "aquafit")
	words: string[]  // text split into words, for fuzzy matching
	stems: string[]  // the words stemmed, for inflection-insensitive matching
}

// stem crudely de-suffixes a word ("skating" and "skate" both become "skat",
// "classes" becomes "class") and folds "mac" onto "mc", so common inflections
// and name variants compare equal. English-biased, but harmless elsewhere
// since both sides are stemmed the same way.
function stem(w: string): string {
	if (w.startsWith('mac')) w = 'mc' + w.slice(3)
	const undouble = (v: string) => (v.length > 2 && v[v.length - 1] === v[v.length - 2] ? v.slice(0, -1) : v) // swimm -> swim
	if (w.length > 5 && w.endsWith('ing')) w = undouble(w.slice(0, -3))
	else if (w.length > 6 && w.endsWith('ers')) w = undouble(w.slice(0, -3))
	else if (w.length > 5 && w.endsWith('er')) w = undouble(w.slice(0, -2))
	else if (w.length > 4 && w.endsWith('es')) w = w.slice(0, -2)
	else if (w.length > 3 && w.endsWith('s') && !w.endsWith('ss')) w = w.slice(0, -1)
	if (w.length > 3 && w.endsWith('e')) w = w.slice(0, -1)
	return w
}

// quickNorm normalizes text for quick-filter matching: on top of normalizeText,
// punctuation collapses to spaces so "st laurent" matches "St-Laurent".
function quickNorm(s: string): string {
	return normalizeText(s).replace(/[^\p{L}\p{N}]+/gu, ' ').trim()
}

// editDist is the Levenshtein distance between a and b, giving up (returning
// max+1) once it exceeds max.
function editDist(a: string, b: string, max: number): number {
	if (Math.abs(a.length - b.length) > max) return max + 1
	let prev = Array.from({length: b.length + 1}, (_, i) => i)
	let cur = new Array<number>(b.length + 1)
	for (let i = 1; i <= a.length; i++) {
		cur[0] = i
		let best = i
		for (let j = 1; j <= b.length; j++) {
			cur[j] = Math.min(prev[j]! + 1, cur[j - 1]! + 1, prev[j - 1]! + (a[i - 1] === b[j - 1] ? 0 : 1))
			best = Math.min(best, cur[j]!)
		}
		if (best > max) return max + 1
		;[prev, cur] = [cur, prev]
	}
	return prev[b.length]!
}

// a quick-filter query word, its stem, and the typo tolerance each earns from
// its length
interface QuickToken {
	t: string
	stem: string
	k: number
	ks: number
}

function quickTokens(q: string): QuickToken[] {
	const fuzz = (n: number) => (n >= 7 ? 2 : n >= 4 ? 1 : 0)
	return quickNorm(q).split(' ').filter(Boolean).map((t) => {
		const st = stem(t)
		return {t, stem: st, k: fuzz(t.length), ks: fuzz(st.length)}
	})
}

// a token matches a session by substring (against the spaced and space-dropped
// text, so partial words and "aqua fit"/"aquafit" both work), by stem prefix
// ("skating" matches "skate", "mcquarrie" matches "MacQuarrie"), or fuzzily
// against each word, same-length word prefix, or stem (so typos still match,
// including in a partially typed word).
function quickTokenMatches(s: Session, tok: QuickToken): boolean {
	if (s.text.includes(tok.t) || s.compact.includes(tok.t)) return true
	for (let i = 0; i < s.words.length; i++) {
		if (s.stems[i]!.startsWith(tok.stem)) return true
		if (!tok.k) continue
		const w = s.words[i]!
		if (editDist(tok.t, w, tok.k) <= tok.k) return true
		if (w.length > tok.t.length && editDist(tok.t, w.slice(0, tok.t.length), tok.k) <= tok.k) return true
		if (tok.ks && editDist(tok.stem, s.stems[i]!, tok.ks) <= tok.ks) return true
	}
	return false
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
	const sessions: Session[] = [...el.querySelectorAll<HTMLElement>('.today-session')].map((s) => {
		const text = quickNorm((s.querySelector('.t-act')?.textContent || '') + ' ' +
			(s.querySelector('.t-fac-name')?.textContent || ''))
		const words = text.split(' ')
		return {
			el: s,
			slug: s.dataset['slug'] || '',
			cats: Number(s.dataset['cats'] || 0),
			start: Number(s.dataset['start'] || 0),
			end: Number(s.dataset['end'] || 0),
			text,
			compact: text.replaceAll(' ', ''),
			words,
			stems: words.map(stem),
		}
	})
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
	q: '', // quick-filter text, as typed (matching normalizes it)
	include: new Set<string>(),
	exclude: new Set<string>(),
	cats: new Set<number>(),
	timeFrom: null as number | null,
	timeTo: null as number | null,
	starredOnly: false,
}

// the tokenized quick-filter query, rebuilt from filter.q on each refresh
let qTokens: QuickToken[] = []

let activeDate = (days.find((d) => d.today) || days[0])?.date || ''

// past sessions on the today tab are hidden by default, with an inline toggle
// at the top of the day to show them (dimmed via the .past class). Client-side
// like the "now" line, since cached pages can't trust the server clock.
let showPast = false
const pastNote = document.createElement('p')
pastNote.className = 'today-past-note'
pastNote.hidden = true
const pastText = document.createElement('span')
const pastBtn = document.createElement('button')
pastBtn.type = 'button'
pastBtn.className = 'msym' // the expand/collapse chevron base (website.css)
pastNote.append(pastText, pastBtn)
pastBtn.addEventListener('click', () => {
	showPast = !showPast
	apply()
})
days.find((d) => d.today)?.el.querySelector('.today-day-head')?.after(pastNote)

// session matching (everything except the per-day weekday split, which the tabs
// handle)
function sessionVisible(s: Session): boolean {
	if (!qTokens.every((t) => quickTokenMatches(s, t))) return false
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
	// wire up the tab <-> panel association (the day section is the panel)
	d.tab.id = 'today-tab-' + d.date
	d.tab.setAttribute('aria-controls', 'today-panel-' + d.date)
	d.el.setAttribute('role', 'tabpanel')
	d.el.id = 'today-panel-' + d.date
	d.el.setAttribute('aria-labelledby', d.tab.id)
	d.el.tabIndex = 0
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

// arrow-key roving focus over the day tabs (horizontal tablist, automatic
// activation: moving focus selects the day). apply() keeps aria-selected and
// the roving tabindex in sync.
tabsEl.addEventListener('keydown', (ev) => {
	const idx = days.findIndex((d) => d.date === activeDate)
	if (idx < 0) return
	let next: number
	switch (ev.key) {
		case 'ArrowRight': next = (idx + 1) % days.length; break
		case 'ArrowLeft': next = (idx - 1 + days.length) % days.length; break
		case 'Home': next = 0; break
		case 'End': next = days.length - 1; break
		default: return
	}
	ev.preventDefault()
	activeDate = days[next]!.date
	apply()
	days[next]!.tab.focus()
})

// refresh the feed: show the active day, filter its sessions (dropping past
// ones on the today tab unless toggled on), and re-mark the "now" line. Also
// run on a timer so sessions drop out as they end.
function refreshFeed() {
	if (!days.some((d) => d.date === activeDate))
		activeDate = (days.find((d) => d.today) || days[0])?.date || ''

	qTokens = quickTokens(filter.q)

	let shownCount = 0
	let pastHidden = 0
	for (const d of days) {
		const isActive = d.date === activeDate
		d.el.hidden = !isActive
		d.tab.classList.toggle('active', isActive)
		d.tab.setAttribute('aria-selected', isActive ? 'true' : 'false')
		d.tab.tabIndex = isActive ? 0 : -1 // roving tabindex
		if (isActive) {
			const nowMins = d.today ? ottawaNowMinutes() : null
			let past = 0
			for (const s of d.sessions) {
				let vis = sessionVisible(s)
				if (vis && nowMins !== null && s.end <= nowMins) {
					past++
					if (!showPast) {
						vis = false
						pastHidden++
					}
				}
				s.el.hidden = !vis
				if (vis) shownCount++
			}
			// hide hour groups left with no visible sessions
			for (const hr of d.el.querySelectorAll<HTMLElement>('.today-hour'))
				hr.hidden = ![...hr.querySelectorAll<HTMLElement>('.today-session')].some((el) => !el.hidden)
			if (d.today) {
				pastNote.hidden = past === 0
				if (past > 0 && nowMins !== null) {
					pastText.textContent = showPast
						? `Sessions that ended before ${clockLabel(nowMins)} are greyed out.`
						: past === 1
							? `1 session that ended before ${clockLabel(nowMins)} is hidden.`
							: `${past} sessions that ended before ${clockLabel(nowMins)} are hidden.`
					pastBtn.textContent = showPast ? 'Hide earlier sessions' : 'Show earlier sessions'
					pastBtn.classList.toggle('open', showPast)
				}
			}
		}
	}
	noResultsEl.hidden = advanced || shownCount > 0 || pastHidden > 0
	markNow()
}

// main apply: refresh the feed and everything else that depends on the filters.
function apply() {
	refreshFeed()
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
	const warn = target.closest<HTMLButtonElement>('[data-warn]')
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
	button.className = 'msym' // the dropdown arrow glyph base (website.css)
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

// quick text filter box, first in the row: matches activity and facility names
// (normalized and typo-tolerant), with an explicit clear button inside it

if (!advanced) {
	const quick = document.createElement('div')
	quick.className = 'today-quick'
	const input = document.createElement('input')
	input.type = 'text'
	input.placeholder = 'Filter by name'
	input.setAttribute('aria-label', 'Filter sessions by activity or facility name')
	const clear = document.createElement('button')
	clear.type = 'button'
	clear.className = 'quick-clear msym'
	clear.title = 'Clear filter'
	clear.setAttribute('aria-label', 'Clear filter')
	clear.hidden = true
	quick.append(input, clear)
	filtersEl.prepend(quick) // before the starred toggle

	function updateButton() {
		clear.hidden = filter.q === ''
		quick.classList.toggle('active', quickTokens(filter.q).length > 0)
	}
	input.addEventListener('input', () => {
		filter.q = input.value
		apply()
	})
	clear.addEventListener('click', () => {
		filter.q = ''
		input.value = ''
		apply()
		input.focus()
	})
	pills.push({updateButton, syncChecks: () => {
		input.value = filter.q
		updateButton()
	}})
}

// active-filter chips

function renderChips() {
	const chips: {label: string; clear: () => void}[] = []
	const facName = (slug: string) => data.facilities.find((f) => f.slug === slug)?.name || slug
	if (filter.q.trim()) chips.push({label: '“' + filter.q.trim() + '”', clear: () => filter.q = ''})
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
			filter.q = ''
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
	if (filter.q.trim()) params.set('f-q', filter.q.trim())
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
	filter.q = params.get('f-q') || ''
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
	modal.classList.remove('wide')
	loadModal(url, btn.dataset['source'])
}

// the full-schedule modal reuses the map's facility fragment, scoped to the
// session's schedule group via ?group.
function openScheduleModal(btn: HTMLElement) {
	const slug = encodeURIComponent(btn.dataset['slug'] || '')
	const group = encodeURIComponent(btn.dataset['group'] || '0')
	modal.classList.add('wide') // room for the full week's schedule table
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
setInterval(refreshFeed, 60_000)
setInterval(relativeUpdated, 60_000)
