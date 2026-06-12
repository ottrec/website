'use strict'
export {}

// Shared store for starred facilities, persisted to localStorage as a
// comma-separated list of facility slugs and synced across tabs. Pages mark
// star toggle buttons with data-fac-star="{slug}" (server-rendered ones start
// hidden until this script reveals them) and passive indicators with
// data-fac-starred="{slug}" (given the .starred class). Other scripts use the
// window.ottrecStarred API; call sync() after inserting fetched content.

const KEY = 'starred'

function load(): Set<string> {
	const v = localStorage.getItem(KEY)
	return new Set(v ? v.split(',').filter(Boolean) : [])
}

function save() {
	if (starred.size) localStorage.setItem(KEY, [...starred].join(','))
	else localStorage.removeItem(KEY)
}

let starred = load()
const listeners: (() => void)[] = []
const wired = new WeakSet<Element>()

function changed() {
	sync()
	for (const fn of listeners) fn()
}

function sync() {
	for (const el of document.querySelectorAll<HTMLElement>('[data-fac-star]')) {
		if (!wired.has(el)) {
			wired.add(el)
			// listen on the button itself rather than delegating so clicks
			// don't also activate the surrounding element (e.g., the map
			// facility list items) and aren't lost to leaflet's popup click
			// propagation blocking
			el.addEventListener('click', (ev) => {
				ev.stopPropagation()
				ev.preventDefault()
				api.toggle(el.dataset['facStar']!)
			})
		}
		const on = starred.has(el.dataset['facStar']!)
		el.hidden = false
		el.classList.toggle('on', on)
		el.setAttribute('aria-pressed', String(on))
		el.title = on ? 'Unstar facility' : 'Star facility'
	}
	for (const el of document.querySelectorAll<HTMLElement>('[data-fac-starred]'))
		el.classList.toggle('starred', starred.has(el.dataset['facStarred']!))
}

const api = {
	has: (slug: string) => starred.has(slug),
	all: () => [...starred],
	count: () => starred.size,
	toggle(slug: string) {
		if (!starred.delete(slug)) starred.add(slug)
		save()
		changed()
	},
	set(slugs: string[]) {
		starred = new Set(slugs.map((s) => s.trim()).filter(Boolean))
		save()
		changed()
	},
	sync,
	onchange(fn: () => void) {
		listeners.push(fn)
	},
}
const w = window as any
w.ottrecStarred = api

// cross-tab sync
window.addEventListener('storage', (ev) => {
	if (ev.key === KEY || ev.key === null) {
		starred = load()
		changed()
	}
})

// save/restore/clear buttons on the about page
function aboutActions() {
	const actions = document.getElementById('starred-actions')
	if (!actions) return
	actions.hidden = false
	document.getElementById('starred-save')!.addEventListener('click', () => {
		prompt('Starred facilities (copy this list to save it):', api.all().join(','))
	})
	document.getElementById('starred-restore')!.addEventListener('click', () => {
		const v = prompt('Paste a saved list of starred facilities:')
		if (v !== null) api.set(v.split(','))
	})
	document.getElementById('starred-clear')!.addEventListener('click', () => {
		if (confirm('Clear all starred facilities?')) api.set([])
	})
}

aboutActions()
sync()
