// Progressive enhancement for the time machine diff page's snapshot range
// selector (.tm-strip). Each bar is a snapshot; its height shows how much
// changed. Without this script the bars are plain links (and the dropdown picker
// below is the no-JS / touch fallback). With it, a live readout follows the
// pointer and the range can be chosen by dragging across the strip (mouse or
// touch) or by tapping the two endpoints. See templates/timemachine.templ.

(function () {
	// Relative timestamps (e.g. the facilities list's last-changed dates). Runs
	// on every page; the server renders the absolute date as a no-JS fallback.
	function relativeTime(d: Date): string {
		const days = Math.round((d.getTime() - Date.now()) / 86400000);
		const rtf = new Intl.RelativeTimeFormat("en", { numeric: "auto" });
		const abs = Math.abs(days);
		if (abs < 1) return rtf.format(0, "day"); // "today"
		if (abs < 30) return rtf.format(days, "day");
		if (abs < 365) return rtf.format(Math.round(days / 30), "month");
		return rtf.format(Math.round(days / 365), "year");
	}
	for (const el of document.querySelectorAll<HTMLTimeElement>("time.tm-reltime[datetime]")) {
		const d = new Date(el.dateTime);
		if (isNaN(d.getTime())) continue;
		if (!el.title) el.title = el.textContent ?? "";
		el.textContent = (el.dataset.prefix ?? "") + relativeTime(d);
	}

	const strip = document.querySelector<HTMLElement>(".tm-strip");
	if (!strip) return;

	const bars = Array.from(strip.querySelectorAll<HTMLAnchorElement>(".tm-bar"));
	if (bars.length === 0) return;

	const only = strip.dataset.only ?? "";

	const idOf = (b: HTMLElement): string => b.dataset.id ?? "";
	const prevOf = (b: HTMLElement): string => b.dataset.prev ?? "";
	const dateOf = (b: HTMLElement): string => b.dataset.date ?? "";
	const magOf = (b: HTMLElement): string => b.dataset.mag ?? "0";
	// bars are rendered oldest → newest, so array index is the time order.
	const indexOf = (b: HTMLElement): number => bars.indexOf(b as HTMLAnchorElement);

	const readout = strip.parentElement?.querySelector<HTMLElement>(".tm-strip-readout") ?? null;
	const defaultReadout = readout?.innerHTML ?? "";

	function showReadout(html: string) {
		if (readout) readout.innerHTML = html;
	}
	function resetReadout() {
		if (readout) readout.innerHTML = defaultReadout;
	}

	function clearMarks() {
		for (const b of bars) {
			b.classList.remove("tm-bar-preview", "tm-bar-picking");
		}
	}

	function markRange(a: HTMLElement, b: HTMLElement) {
		const lo = Math.min(indexOf(a), indexOf(b));
		const hi = Math.max(indexOf(a), indexOf(b));
		for (let i = 0; i < bars.length; i++) {
			bars[i].classList.toggle("tm-bar-preview", i >= lo && i <= hi);
		}
	}

	// older bar (lower index) → newer bar.
	function ends(a: HTMLElement, b: HTMLElement): [HTMLElement, HTMLElement] {
		return indexOf(a) <= indexOf(b) ? [a, b] : [b, a];
	}
	function rangeLabel(a: HTMLElement, b: HTMLElement): string {
		const [older, newer] = ends(a, b);
		return dateOf(older) + " → " + dateOf(newer);
	}

	function navigateIds(from: string, to: string) {
		if (!from || !to) return;
		let href = "/datasets?from=" + encodeURIComponent(from) + "&to=" + encodeURIComponent(to);
		if (only) href += "&only=" + encodeURIComponent(only);
		location.href = href;
	}
	function navigate(a: HTMLElement, b: HTMLElement) {
		const [older, newer] = ends(a, b);
		navigateIds(idOf(older), idOf(newer));
	}

	// the bar under a client point (pointermove fires on the captured target, so
	// resolve the bar from coordinates instead).
	function barAt(x: number, y: number): HTMLAnchorElement | null {
		const el = document.elementFromPoint(x, y);
		return (el?.closest(".tm-bar") as HTMLAnchorElement) ?? null;
	}

	let pending: HTMLAnchorElement | null = null; // first endpoint of a two-tap selection
	let dragStart: HTMLAnchorElement | null = null; // anchor of an in-progress drag
	let dragged = false;

	function hover(bar: HTMLElement) {
		if (pending) {
			markRange(pending, bar);
			showReadout(rangeLabel(pending, bar));
		} else {
			showReadout(dateOf(bar) + ' <span class="tm-strip-mag">' + magOf(bar) + " changed</span>");
		}
	}

	strip.addEventListener("pointerdown", (e) => {
		const bar = barAt(e.clientX, e.clientY);
		if (!bar) return;
		e.preventDefault(); // suppress the fallback link + native text selection
		dragStart = bar;
		dragged = false;
		try {
			strip.setPointerCapture(e.pointerId);
		} catch {
			/* not capturable; fine */
		}
	});

	strip.addEventListener("pointermove", (e) => {
		const bar = barAt(e.clientX, e.clientY);
		if (!bar) return;
		if (dragStart) {
			if (bar !== dragStart) dragged = true;
			markRange(dragStart, bar);
			showReadout(rangeLabel(dragStart, bar));
		} else {
			hover(bar);
		}
	});

	strip.addEventListener("pointerup", (e) => {
		const bar = barAt(e.clientX, e.clientY) ?? dragStart;
		const start = dragStart;
		dragStart = null;
		clearMarks();
		if (!bar) return;

		if (dragged && start && bar !== start) {
			navigate(start, bar); // brush selection
			return;
		}
		// a tap: two-tap endpoint selection.
		if (pending === null) {
			pending = bar;
			bar.classList.add("tm-bar-picking");
			showReadout(dateOf(bar) + " → …");
		} else if (pending === bar) {
			// tapping the same bar twice diffs that snapshot against the previous one.
			pending = null;
			navigateIds(prevOf(bar) || idOf(bar), idOf(bar));
		} else {
			navigate(pending, bar);
		}
	});

	strip.addEventListener("pointerleave", () => {
		if (dragStart) return; // mid-drag (with capture); ignore
		if (pending) {
			clearMarks();
			pending.classList.add("tm-bar-picking");
			showReadout(dateOf(pending) + " → …");
		} else {
			resetReadout();
		}
	});

	// Escape cancels a pending tap selection.
	document.addEventListener("keydown", (e) => {
		if (e.key === "Escape" && pending !== null) {
			pending = null;
			clearMarks();
			resetReadout();
		}
	});
})();
