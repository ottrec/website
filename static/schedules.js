'use strict';
(function () {

// progressively enhance the advanced search with live query validation; the
// form still works without this (errors are shown when it is submitted)

const form = document.querySelector('.schedules-search-advanced');
if (!form) return;
const textarea = form.querySelector('textarea[name="q"]');
if (!textarea) return;

const errEl = document.createElement('p');
errEl.className = 'schedules-query-error';
errEl.setAttribute('aria-live', 'polite');
errEl.hidden = true;
textarea.after(errEl);

let timer = null, ctrl = null, seq = 0;

function show(msg) {
	errEl.textContent = msg || '';
	errEl.hidden = !msg;
}

async function validate() {
	const q = textarea.value.trim();
	const token = ++seq;
	if (ctrl) ctrl.abort();
	if (!q) return show('');
	ctrl = new AbortController();
	try {
		const resp = await fetch('/api/ottrecql/validate?q=' + encodeURIComponent(q), {signal: ctrl.signal});
		if (!resp.ok) throw new Error('status ' + resp.status);
		const data = await resp.json();
		if (token === seq) show(data.error);
	} catch (err) {
		// don't get in the way if validation itself fails
		if (token === seq && err.name !== 'AbortError') show('');
	}
}

textarea.addEventListener('input', () => {
	clearTimeout(timer);
	timer = setTimeout(validate, 350);
});

})();
