'use strict';
(function () {

// light/dark/auto toggle in the navbar, saved in localStorage; the saved
// override is applied before first paint by an inline script in the head, and
// everything follows it via css color-scheme/light-dark() (the map tiles
// listen for the themechange event)

const KEY = 'theme';
const LABELS = {light: 'Light', dark: 'Dark', auto: 'Auto'};

const btn = document.getElementById('theme-toggle');
if (!btn) return;

function current() {
	try {
		const t = localStorage.getItem(KEY);
		if (t === 'light' || t === 'dark') return t;
	} catch (err) {}
	return 'auto';
}

function apply(t) {
	document.documentElement.style.colorScheme = (t === 'light' || t === 'dark') ? t : '';
	btn.dataset.theme = t; // the icon is set from this in css
	btn.setAttribute('aria-label', 'Color scheme: ' + LABELS[t]);
	window.dispatchEvent(new CustomEvent('themechange'));
}

btn.addEventListener('click', () => {
	const next = {auto: 'light', light: 'dark', dark: 'auto'}[current()];
	try {
		if (next === 'auto') localStorage.removeItem(KEY);
		else localStorage.setItem(KEY, next);
	} catch (err) {}
	apply(next);
});

// follow changes made in other tabs
window.addEventListener('storage', (ev) => {
	if (ev.key === KEY || ev.key === null) apply(current());
});

btn.hidden = false;
apply(current());

})();
