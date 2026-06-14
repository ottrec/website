// The shared store for starred facilities, implemented by starred.ts and
// exposed on the window for the other page scripts. See starred.ts for the
// protocol and the data-fac-star/data-fac-starred markup it drives.
interface OttrecStarred {
	has(slug: string): boolean
	all(): string[]
	count(): number
	toggle(slug: string): void
	set(slugs: string[]): void
	sync(): void
	onchange(fn: () => void): void
}

// starred.ts publishes the store here; other scripts read it as a global.
declare const ottrecStarred: OttrecStarred

interface Window {
	ottrecStarred: OttrecStarred
}
