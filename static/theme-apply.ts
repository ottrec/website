'use strict'

export type Theme = 'light' | 'dark' | 'auto'

export const KEY = 'theme'

export function savedTheme(): Theme {
	try {
		const t = localStorage.getItem(KEY)
		if (t === 'light' || t === 'dark') return t
	} catch {}
	return 'auto'
}

export function applyColorScheme(t: Theme) {
	const s = document.documentElement.style
	s.colorScheme = t === 'auto' ? 'light dark' : t
	if (t === 'auto') {
		s.removeProperty('--lightningcss-light')
		s.removeProperty('--lightningcss-dark')
	} else {
		s.setProperty('--lightningcss-light', t === 'light' ? 'initial' : ' ')
		s.setProperty('--lightningcss-dark', t === 'dark' ? 'initial' : ' ')
	}
}

// this script is also inlined into HEAD so it doesn't flash the wrong theme
applyColorScheme(savedTheme())
