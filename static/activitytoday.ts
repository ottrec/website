'use strict'

import {quickTarget, quickTokens, quickMatch, type QuickTarget} from './quickfilter'

// Enhancements for the activity landing page's minimal today widget:
//  - client-side filtering by facility/activity text and a start/end time window
//  - an expand toggle that makes the scroll box taller
//  - opening the session warning/reservation modals (the same /api/changes,
//    /api/errors, /api/reservations fragments the full /today page uses)
//  - starred facilities first in the facilities section (starred.js must load
//    before this script)
// The list is server-rendered and works without JS; this only enhances it.

// --- filtering ---

const list = document.getElementById('activity-today-list')
if (list) {
	const q = document.getElementById('activity-today-q') as HTMLInputElement | null
	const from = document.getElementById('activity-today-from') as HTMLInputElement | null
	const to = document.getElementById('activity-today-to') as HTMLInputElement | null
	const noMatch = document.getElementById('activity-today-nomatch')
	const count = document.getElementById('activity-today-count')
	const clear = document.getElementById('activity-today-clear')
	const nowBtn = document.getElementById('activity-today-now')

	// the filter inputs are server-hidden since they need JS
	const filters = document.querySelector<HTMLElement>('.activity-today-filters')
	if (filters) filters.hidden = false
	// precompute each row's fuzzy-searchable target (activity + facility) and its
	// time window, matching the /today feed's quick filter
	interface Row {
		li: HTMLLIElement
		target: QuickTarget
		start: number
		end: number
	}
	const rows: Row[] = [...list.querySelectorAll<HTMLLIElement>('li')].map((li) => ({
		li,
		target: quickTarget((li.dataset['act'] || '') + ' ' + (li.dataset['fac'] || '')),
		start: +(li.dataset['start'] || '0'),
		end: +(li.dataset['end'] || '0'),
	}))

	// "HH:MM" -> minutes from midnight, or null when empty/invalid
	const parseTime = (v: string): number | null => {
		const m = /^(\d{1,2}):(\d{2})$/.exec(v)
		return m ? +m[1]! * 60 + +m[2]! : null
	}

	const apply = () => {
		const tokens = quickTokens(q?.value || '')
		const fromMin = parseTime(from?.value || '')
		const toMin = parseTime(to?.value || '')
		let shown = 0
		for (const r of rows) {
			let ok = true
			if (tokens.length && !quickMatch(r.target, tokens)) ok = false
			if (fromMin !== null && r.start < fromMin) ok = false
			if (toMin !== null && r.end > toMin) ok = false
			r.li.hidden = !ok
			if (ok) shown++
		}
		if (noMatch) noMatch.hidden = shown !== 0
		// the banner always shows the count; shown/total (and the clear button)
		// only while a filter is active
		const active = tokens.length > 0 || fromMin !== null || toMin !== null
		if (count)
			count.textContent = active
				? shown + '/' + rows.length + ' sessions'
				: rows.length + (rows.length === 1 ? ' session' : ' sessions')
		if (clear) clear.hidden = !active
	}

	q?.addEventListener('input', apply)
	from?.addEventListener('input', apply)
	to?.addEventListener('input', apply)

	const clearFilters = () => {
		if (q) q.value = ''
		if (from) from.value = ''
		if (to) to.value = ''
		apply()
	}

	clear?.addEventListener('click', clearFilters)

	// the Now button clears the time window and jumps back to the current
	// session
	nowBtn?.addEventListener('click', () => {
		if (from) from.value = ''
		if (to) to.value = ''
		apply()
		jumpToNow(true)
	})

	// don't retain filter values across reloads or bfcache restores
	window.addEventListener('pageshow', clearFilters)
}

// --- session states + scroll to now ---

// grey out sessions that have ended and tint the ones on now (the same classes
// the /today feed uses), then start the list at the first session that hasn't
// ended yet, so the visitor sees what's on now rather than this morning's
// sessions. Session times are in Ottawa time (same trick as today.ts). The Now
// button re-runs this, skipping filtered-out rows.
function jumpToNow(smooth?: boolean) {
	const box = document.querySelector<HTMLElement>('.activity-today-scroll')
	const items = box ? [...box.querySelectorAll<HTMLLIElement>('.today-session')] : []
	if (!box || !items.length) return
	const parts = new Intl.DateTimeFormat('en-GB', {
		timeZone: 'America/Toronto', hour: '2-digit', minute: '2-digit', hour12: false,
	}).formatToParts(new Date())
	const get = (t: string) => Number(parts.find((p) => p.type === t)?.value || 0)
	const now = (get('hour') % 24) * 60 + get('minute')
	for (const li of items) {
		li.classList.remove('now', 'past')
		if (+(li.dataset['end'] || '0') <= now) li.classList.add('past')
		else if (+(li.dataset['start'] || '0') <= now) li.classList.add('now')
	}
	const visible = items.filter((li) => !li.hidden)
	const target = visible.find((li) => +(li.dataset['end'] || '0') > now)
	let top = 0
	if (!target) {
		top = box.scrollHeight // everything's over; show the evening
	} else if (target !== visible[0]) {
		// a sliver of the previous session stays visible under the shadow,
		// reinforcing that the list scrolls
		top = target.getBoundingClientRect().top - box.getBoundingClientRect().top + box.scrollTop - 8
	}
	box.scrollTo({top, behavior: smooth ? 'smooth' : 'auto'})
}
jumpToNow()

// --- expand toggle ---

const scroll = document.querySelector<HTMLElement>('.activity-today-scroll')
const expandBtn = document.getElementById('activity-today-expand')
const expandLabel = expandBtn?.querySelector('.activity-today-expand-label')
if (scroll && expandBtn) {
	expandBtn.hidden = false // server-hidden since it needs JS
	expandBtn.addEventListener('click', () => {
		const expanded = scroll.classList.toggle('expanded')
		expandBtn.setAttribute('aria-expanded', String(expanded))
		if (expandLabel) expandLabel.textContent = expanded ? 'Collapse' : 'Expand'
	})
}

// --- sticky sidebar height ---

// the wide-layout sticky box (the sidebar aside, or the today section alone on
// short screens where the map moves to the main column) should fill the
// viewport below its current on-screen top, which starts under the navbars and
// rises to the sticky offset as they scroll away. CSS alone can't express
// that, so its fallback height just leaves room for the navbars; this measures
// the real position and keeps the height exact.
{
	const boxes = [
		document.querySelector<HTMLElement>('.activity-side'),
		document.querySelector<HTMLElement>('.activity-today'),
	].filter((el) => el !== null)
	const mq = window.matchMedia('(min-width: 64rem)') // the wide-layout breakpoint
	if (boxes.length) {
		let raf = 0
		const update = () => {
			raf = 0
			for (const el of boxes) el.style.height = ''
			if (!mq.matches) return
			const box = boxes.find((el) => getComputedStyle(el).position === 'sticky')
			if (!box) return
			const top = box.getBoundingClientRect().top
			box.style.height = 'calc(100vh - ' + Math.max(0, top) + 'px - 1.5rem)'
		}
		const schedule = () => {
			if (!raf) raf = requestAnimationFrame(update)
		}
		update()
		window.addEventListener('scroll', schedule, {passive: true})
		window.addEventListener('resize', schedule)
		mq.addEventListener('change', schedule)
	}
}

// --- starred facilities ---

// move starred facilities to the front of each sector's list, on load only
// (like the schedules pages; a live re-sort would jump the page mid-read)
function sortStarred() {
	for (const ul of document.querySelectorAll('.activity-sector ul')) {
		const items = [...ul.querySelectorAll<HTMLElement>(':scope > li')]
		const starred = (li: HTMLElement) => {
			const slug = li.querySelector<HTMLElement>('[data-fac-star]')?.dataset['facStar']
			return Boolean(slug && ottrecStarred.has(slug))
		}
		if (!items.some(starred)) continue
		for (const li of [...items.filter(starred), ...items.filter((li) => !starred(li))]) ul.append(li)
	}
}
sortStarred()

// --- warning / reservation modals ---

let dialog: HTMLDialogElement | null = null
let dcontent: HTMLElement
let token = 0

function ensureDialog(): HTMLDialogElement {
	if (dialog) return dialog
	const d = document.createElement('dialog')
	d.className = 'today-modal'
	d.innerHTML =
		'<button type="button" class="today-modal-close" aria-label="Close">×</button>' +
		'<div class="today-modal-content"></div>'
	document.body.append(d)
	dcontent = d.querySelector<HTMLElement>('.today-modal-content')!
	d.querySelector<HTMLButtonElement>('.today-modal-close')!.addEventListener('click', () => d.close())
	d.addEventListener('click', (ev) => {
		if (ev.target === d) d.close()
	})
	d.addEventListener('close', () => {
		document.body.style.overflow = ''
	})
	dialog = d
	return d
}

async function openWarn(url: string) {
	const d = ensureDialog()
	const t = ++token
	dcontent.innerHTML = '<div class="today-modal-body"><p>Loading…</p></div>'
	document.body.style.overflow = 'hidden'
	if (!d.open) d.showModal()
	dcontent.scrollTop = 0
	try {
		const resp = await fetch(url)
		if (!resp.ok) throw new Error('status ' + resp.status)
		const html = await resp.text()
		if (t !== token) return
		dcontent.innerHTML = html
		dcontent.scrollTop = 0
	} catch {
		if (t !== token) return
		dcontent.innerHTML =
			'<div class="today-modal-body"><p>Couldn’t load this. <a href="/today">Open the today page.</a></p></div>'
	}
}

// modal openers carry data-warn/data-slug/data-group, mirroring the today
// page: the session notice chips (links to the facility page as the no-JS
// fallback) and the whole rows of the cancellations/notices lists. Plain
// clicks open the modal; modified clicks keep the link behavior.
document.addEventListener('click', (ev) => {
	const btn = (ev.target as HTMLElement).closest<HTMLElement>('[data-warn]')
	if (!btn) return
	// a link or button nested inside (e.g. the facility link within a
	// clickable row) keeps its own behavior
	const inner = (ev.target as HTMLElement).closest('a, button')
	if (inner && inner !== btn && btn.contains(inner)) return
	if (ev.ctrlKey || ev.metaKey || ev.shiftKey || ev.altKey) return
	ev.preventDefault()
	const slug = encodeURIComponent(btn.dataset['slug'] || '')
	const group = encodeURIComponent(btn.dataset['group'] || '')
	const kind = btn.dataset['warn']
	const url =
		kind === 'errors'
			? '/api/errors?facility=' + slug
			: kind === 'reservations'
				? '/api/reservations?facility=' + slug + '&group=' + group
				: '/api/changes?facility=' + slug + '&group=' + group
	openWarn(url)
})
