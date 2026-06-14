'use strict'
export {}

// the shared starred facility store (starred.js)
declare const ottrecStarred: {
	has(slug: string): boolean
	all(): string[]
	count(): number
	toggle(slug: string): void
	set(slugs: string[]): void
	sync(): void
	onchange(fn: () => void): void
}

// move starred facilities to the front of each activity table, keeping the
// alphabetical order within each group; on load only, so the rows don't jump
// around when a star is toggled mid-read
function sortStarred() {
	for (const tbody of document.querySelectorAll<HTMLElement>('.activity-table tbody')) {
		const rows = [...tbody.querySelectorAll<HTMLElement>(':scope > tr')]
		const starred = (tr: HTMLElement) => {
			const slug = tr.querySelector<HTMLElement>('[data-fac-star]')?.dataset['facStar']
			return Boolean(slug && ottrecStarred.has(slug))
		}
		if (!rows.some(starred)) continue
		for (const tr of [...rows.filter(starred), ...rows.filter((tr) => !starred(tr))])
			tbody.append(tr)
	}
}

sortStarred()
