'use strict'
import {createModal, type Modal} from './modal'

// Ephemeral facility-schedule modal. Facility links across the site are real
// anchors to /schedules/facility/{slug} (marked data-facility-modal={slug}) and
// work without JS. This progressively enhances a plain primary click on one to
// open the facility's full schedule in the shared modal shell (modal.ts)
// instead of navigating: no URL change, no history entry. Middle-click,
// modified clicks, and "open in new tab" are left alone so the anchor behaves
// normally.

let modal: Modal | null = null
let token = 0

async function openModal(slug: string) {
	modal ??= createModal('facmodal wide', 'Facility schedule')
	const t = ++token
	modal.dialog.setAttribute('aria-label', 'Facility schedule')
	modal.content.innerHTML = '<p class="facmodal-loading">Loading…</p>'
	modal.open()
	modal.content.scrollTop = 0
	try {
		const resp = await fetch('/api/facility?facility=' + encodeURIComponent(slug))
		if (!resp.ok) throw new Error('status ' + resp.status)
		const html = await resp.text()
		if (t !== token) return
		modal.content.innerHTML = html
		modal.content.scrollTop = 0
		const name = modal.content.querySelector('.facility-name')?.textContent?.trim()
		if (name) modal.dialog.setAttribute('aria-label', name)
	} catch {
		if (t !== token) return
		modal.content.innerHTML = '<p class="facmodal-empty">Couldn’t load the schedule. ' +
			'<a href="/schedules/facility/' + encodeURIComponent(slug) + '">Open the full page.</a></p>'
	}
}

// intercept only a plain primary click; anything else (middle-click, modified
// click, an already-handled click) navigates the anchor as usual
document.addEventListener('click', (ev) => {
	if (ev.button !== 0 || ev.ctrlKey || ev.metaKey || ev.shiftKey || ev.altKey || ev.defaultPrevented) return
	const link = (ev.target as HTMLElement).closest<HTMLAnchorElement>('a[data-facility-modal]')
	const slug = link?.dataset['facilityModal']
	if (!slug) return
	ev.preventDefault()
	openModal(slug)
})

// expose the opener so other scripts (e.g. the activity page's minimal map) can
// open the facility schedule directly instead of via an anchor click
;(window as unknown as {ottrecFacilityModal?: (slug: string) => void}).ottrecFacilityModal = openModal
