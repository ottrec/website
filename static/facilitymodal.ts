'use strict'

// Ephemeral facility-schedule modal. Facility links across the site are real
// anchors to /schedules/facility/{slug} (marked data-facility-modal={slug}) and
// work without JS. This progressively enhances a plain primary click on one to
// open the facility's full schedule in a <dialog> overlay instead of navigating:
// no URL change, no history entry. Middle-click, modified clicks, and "open in
// new tab" are left alone so the anchor behaves normally.
//
// The native <dialog>.showModal() gives us the focus trap, Escape-to-close, and
// focus return to the trigger for free; we add the role/aria hints, backdrop
// close, background scroll lock, and the fragment fetch.

let dialog: HTMLDialogElement | null = null
let content: HTMLElement
let token = 0

function ensureDialog(): HTMLDialogElement {
	if (dialog) return dialog
	const d = document.createElement('dialog')
	d.className = 'facmodal'
	d.setAttribute('role', 'dialog')
	d.setAttribute('aria-modal', 'true')
	d.setAttribute('aria-label', 'Facility schedule')
	d.innerHTML =
		'<button type="button" class="facmodal-close" aria-label="Close">×</button>' +
		'<div class="facmodal-content"></div>'
	document.body.append(d)
	content = d.querySelector<HTMLElement>('.facmodal-content')!
	d.querySelector<HTMLButtonElement>('.facmodal-close')!.addEventListener('click', () => d.close())
	// a click on the dialog element itself (its padding-free box only wraps the
	// content, so this is the ::backdrop) closes it
	d.addEventListener('click', (ev) => {
		if (ev.target === d) d.close()
	})
	// showModal doesn't lock background scroll; do it ourselves and restore on
	// close (covers the close button, backdrop, and Escape's native close)
	d.addEventListener('close', () => {
		document.body.style.overflow = ''
	})
	dialog = d
	return d
}

async function openModal(slug: string) {
	const d = ensureDialog()
	const t = ++token
	d.setAttribute('aria-label', 'Facility schedule')
	content.innerHTML = '<p class="facmodal-loading">Loading…</p>'
	document.body.style.overflow = 'hidden'
	if (!d.open) d.showModal()
	content.scrollTop = 0
	try {
		const resp = await fetch('/api/facility?facility=' + encodeURIComponent(slug))
		if (!resp.ok) throw new Error('status ' + resp.status)
		const html = await resp.text()
		if (t !== token) return
		content.innerHTML = html
		content.scrollTop = 0
		const name = content.querySelector('.facility-name')?.textContent?.trim()
		if (name) d.setAttribute('aria-label', name)
	} catch {
		if (t !== token) return
		content.innerHTML = '<p class="facmodal-empty">Couldn’t load the schedule. ' +
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
