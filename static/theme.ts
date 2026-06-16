'use strict'
import {applyColorScheme, savedTheme, KEY, type Theme} from './theme-apply'

const LABELS: Record<Theme, string> = {
	light: 'Light',
	dark: 'Dark',
	auto: 'Auto',
} as const

const btn = document.getElementById('theme-toggle')
if (btn) init(btn)

function init(btn: HTMLElement) {
	function apply(t: Theme) {
		applyColorScheme(t)
		btn.dataset['theme'] = t // the icon is set from this in css
		btn.setAttribute('aria-label', 'Color scheme: ' + LABELS[t])
		window.dispatchEvent(new CustomEvent('themechange'))
	}

	btn.addEventListener('click', () => {
		const next = ({light: 'dark', dark: 'auto', auto: 'light'} as const)[savedTheme()]
		try {
			// 'auto' is stored explicitly since the default (no key) is now light
			localStorage.setItem(KEY, next)
		} catch {}
		apply(next)
	})

	// follow changes made in other tabs
	window.addEventListener('storage', (ev) => {
		if (ev.key === KEY || ev.key === null) apply(savedTheme())
	})

	btn.hidden = false
	apply(savedTheme())
}
