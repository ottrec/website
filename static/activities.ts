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

groupStarred()
