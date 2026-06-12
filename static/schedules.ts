'use strict'
export {}

// progressively enhance the advanced search with live query validation; the
// form still works without this (errors are shown when it is submitted)
function liveValidation() {
	const form = document.querySelector('.schedules-search-advanced')
	if (!form) return
	const textarea = form.querySelector<HTMLTextAreaElement>('textarea[name="q"]')
	if (!textarea) return

	const errEl = document.createElement('p')
	errEl.className = 'schedules-query-error'
	errEl.setAttribute('aria-live', 'polite')
	errEl.hidden = true
	textarea.after(errEl)

	let timer: ReturnType<typeof setTimeout> | undefined, ctrl: AbortController | null = null, seq = 0

	function show(msg: string) {
		errEl.textContent = msg || ''
		errEl.hidden = !msg
	}

	async function validate() {
		const q = textarea!.value.trim()
		const token = ++seq
		if (ctrl) ctrl.abort()
		if (!q) return show('')
		ctrl = new AbortController()
		try {
			const resp = await fetch('/api/ottrecql/validate?q=' + encodeURIComponent(q), {signal: ctrl.signal})
			if (!resp.ok) throw new Error('status ' + resp.status)
			const data = await resp.json()
			if (token === seq) show(data.error)
		} catch (err: any) {
			// don't get in the way if validation itself fails
			if (token === seq && err.name !== 'AbortError') show('')
		}
	}

	textarea.addEventListener('input', () => {
		clearTimeout(timer)
		timer = setTimeout(validate, 350)
	})
}

// highlight the currently visible facility/schedule group in the sidebar toc,
// keeping it scrolled into view
function tocTracking() {
	const toc = document.querySelector('.schedules-toc')
	if (!toc) return

	const links = new Map<string, HTMLAnchorElement>() // anchor id -> toc link
	for (const a of toc.querySelectorAll<HTMLAnchorElement>('a[href^="#"]'))
		links.set(decodeURIComponent(a.getAttribute('href')!.slice(1)), a)
	const targets = [...links.keys()].map((id) => document.getElementById(id)).filter((t): t is HTMLElement => Boolean(t))
	if (!targets.length) return

	function facilityLink(a: HTMLAnchorElement) {
		const li = a.closest('.schedules-toc > nav > ul > li')
		return li ? li.querySelector<HTMLAnchorElement>(':scope > a') : null
	}

	// scroll the toc itself (not the page) to keep the link visible
	function reveal(a: HTMLAnchorElement) {
		const r = a.getBoundingClientRect(), c = toc!.getBoundingClientRect()
		if (r.top < c.top + 8) toc!.scrollTop += r.top - c.top - 24
		else if (r.bottom > c.bottom - 8) toc!.scrollTop += r.bottom - c.bottom + 24
	}

	let lastLink: HTMLAnchorElement | null = null, lastFac: HTMLAnchorElement | null = null
	function update() {
		// the current section is the last one starting in the top third of the
		// viewport, or the very last one once scrolled to the bottom
		const threshold = window.innerHeight * .3
		let current: HTMLElement | null = null
		for (const t of targets)
			if (t.getBoundingClientRect().top <= threshold) current = t
		if (window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - 2)
			current = targets[targets.length - 1]!

		const link = (current && links.get(current.id)) || null
		const fac = link ? facilityLink(link) : null
		if (link === lastLink && fac === lastFac) return
		if (lastLink) lastLink.classList.remove('active')
		if (lastFac && lastFac !== lastLink) lastFac.classList.remove('active')
		if (link) link.classList.add('active')
		if (fac && fac !== link) fac.classList.add('active')
		lastLink = link
		lastFac = fac
		if (link) reveal(link)
	}

	let raf = 0
	function schedule() {
		if (!raf) raf = requestAnimationFrame(() => {
			raf = 0
			update()
		})
	}
	window.addEventListener('scroll', schedule, {passive: true})
	window.addEventListener('resize', schedule, {passive: true})
	update()
}

liveValidation()
tocTracking()
