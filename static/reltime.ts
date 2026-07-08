'use strict'

// Replace server-rendered absolute "last updated" timestamps (marked
// data-reltime) with a relative label, keeping the absolute time as the title.
// Mirrors the /today page's relativeUpdated behaviour. Progressive enhancement:
// without JS the absolute time stays.

function relativeUpdated(el: HTMLTimeElement) {
	if (!el.dateTime) return
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

for (const el of document.querySelectorAll<HTMLTimeElement>('time[data-reltime]')) relativeUpdated(el)
