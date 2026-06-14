'use strict'
export {}

// the shared starred facility store (starred.ts) is a global; see ottrecstarred.d.ts

// The tables are grouped into sector <tbody>s server-side, each led by a sector
// header row. On load (only, so rows don't jump when a star is toggled
// mid-read) we lift the starred facilities out into a "Starred" group at the
// top, keeping their order, and hide any sector group left empty.
function groupStarred() {
	const COLS = 22 // facility column + 7 weekdays x 3 periods (matches the templ)
	for (const table of document.querySelectorAll<HTMLTableElement>('.activity-table table')) {
		const isStarred = (tr: HTMLElement) => {
			const slug = tr.querySelector<HTMLElement>('[data-fac-star]')?.dataset['facStar']
			return Boolean(slug && ottrecStarred.has(slug))
		}
		const bodies = [...table.tBodies]
		const starred: HTMLTableRowElement[] = []
		for (const body of bodies)
			for (const tr of [...body.rows])
				if (isStarred(tr)) starred.push(tr)
		if (!starred.length) continue

		const group = document.createElement('tbody')
		group.className = 'sector-starred'
		const head = group.insertRow()
		head.className = 'sector-row'
		const th = document.createElement('th')
		th.className = 'sector'
		th.colSpan = COLS
		th.scope = 'colgroup'
		th.textContent = '★ Starred'
		head.append(th)
		table.insertBefore(group, bodies[0]!)
		for (const tr of starred) group.append(tr)

		// hide sector groups whose facilities were all moved into Starred
		for (const body of bodies)
			if (![...body.rows].some((tr) => tr.querySelector('[data-fac-star]')))
				body.hidden = true
	}
}

// highlightNow tints the column for the current weekday and time of day (in
// Ottawa time, where the schedules are), so "right now" stands out. Done
// client-side (not server) so cached pages are correct, and refreshed each
// minute so a long-open tab keeps up as the period/day rolls over.
function highlightNow() {
	const tz = 'America/Toronto'
	const now = new Date()
	const days = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']
	const day = days.indexOf(new Intl.DateTimeFormat('en-US', {timeZone: tz, weekday: 'short'}).format(now))
	const [hh, mm] = new Intl.DateTimeFormat('en-GB', {timeZone: tz, hour: '2-digit', minute: '2-digit', hour12: false}).format(now).split(':')
	const mins = Number(hh) * 60 + Number(mm)
	const period = mins < 11 * 60 ? 0 : mins < 17 * 60 ? 1 : 2
	const col = period * 7 + day // index into the period-major columns (0..20)

	for (const el of document.querySelectorAll('.activity-table .now')) el.classList.remove('now')
	if (day < 0) return
	for (const table of document.querySelectorAll<HTMLTableElement>('.activity-table table')) {
		table.tHead?.rows[1]?.cells[col]?.classList.add('now')
		for (const body of table.tBodies)
			for (const row of body.rows) {
				const tds = row.querySelectorAll<HTMLTableCellElement>('td')
				if (tds.length) tds[col]?.classList.add('now')
			}
	}
}

groupStarred()
highlightNow()
setInterval(highlightNow, 60_000)
