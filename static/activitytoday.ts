'use strict'

import {quickTarget, quickTokens, quickMatch, type QuickTarget} from './quickfilter'

// Enhancements for the activity landing page's minimal today widget:
//  - client-side filtering by facility/activity text and a start/end time window
//  - an expand toggle that makes the scroll box taller
//  - opening the session warning/reservation modals (the same /api/changes,
//    /api/errors, /api/reservations fragments the full /today page uses)
// The list is server-rendered and works without JS; this only enhances it.

// --- filtering ---

const list = document.getElementById('activity-today-list')
if (list) {
	const q = document.getElementById('activity-today-q') as HTMLInputElement | null
	const from = document.getElementById('activity-today-from') as HTMLInputElement | null
	const to = document.getElementById('activity-today-to') as HTMLInputElement | null
	const noMatch = document.getElementById('activity-today-nomatch')
	const bar = document.getElementById('activity-today-filterbar')
	const count = document.getElementById('activity-today-count')
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
		// banner: only while a filter is active, showing shown/total
		const active = tokens.length > 0 || fromMin !== null || toMin !== null
		if (count) count.textContent = shown + '/' + rows.length + ' sessions'
		if (bar) bar.hidden = !active
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

	document.getElementById('activity-today-clear')?.addEventListener('click', clearFilters)

	// don't retain filter values across reloads or bfcache restores
	window.addEventListener('pageshow', clearFilters)
}

// --- expand toggle ---

const scroll = document.querySelector<HTMLElement>('.activity-today-scroll')
const expandBtn = document.getElementById('activity-today-expand')
const expandLabel = expandBtn?.querySelector('.activity-today-expand-label')
if (scroll && expandBtn) {
	expandBtn.addEventListener('click', () => {
		const expanded = scroll.classList.toggle('expanded')
		expandBtn.setAttribute('aria-expanded', String(expanded))
		if (expandLabel) expandLabel.textContent = expanded ? 'Collapse' : 'Expand'
	})
}

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

// the warning lines (from todaySessionWarnings/todaySessionReservation) are
// buttons carrying data-warn/data-slug/data-group, mirroring the today page
document.addEventListener('click', (ev) => {
	const btn = (ev.target as HTMLElement).closest<HTMLButtonElement>('button[data-warn]')
	if (!btn) return
	const slug = encodeURIComponent(btn.dataset['slug'] || '')
	const group = encodeURIComponent(btn.dataset['group'] || '0')
	const kind = btn.dataset['warn']
	const url =
		kind === 'errors'
			? '/api/errors?facility=' + slug
			: kind === 'reservations'
				? '/api/reservations?facility=' + slug + '&group=' + group
				: '/api/changes?facility=' + slug + '&group=' + group
	openWarn(url)
})
