'use strict'

// Shared modal shell (styles in website.css): a <dialog> stretched over the
// whole viewport so its own background is the scrim (::backdrop renders
// unreliably on some devices), with the visible box inside and subtle
// open/close fades. showModal() gives us the focus trap and focus return; we
// add the scrim click, animated close, and background scroll lock.

export interface Modal {
	dialog: HTMLDialogElement
	content: HTMLElement
	open(): void
	close(): void
}

const reducedMotion = matchMedia('(prefers-reduced-motion: reduce)')

// createModal builds a dialog with the shared shell markup and returns it with
// its content container and open/close, which play the enter/exit animations.
// className is added to the dialog ("wide" for the wide layout); label is the
// initial aria-label (callers may update it per content).
export function createModal(className: string, label: string): Modal {
	const d = document.createElement('dialog')
	d.className = 'modal' + (className ? ' ' + className : '')
	d.setAttribute('role', 'dialog')
	d.setAttribute('aria-modal', 'true')
	d.setAttribute('aria-label', label)
	d.innerHTML =
		'<div class="modal-box">' +
		'<button type="button" class="modal-close" aria-label="Close">×</button>' +
		'<div class="modal-content"></div>' +
		'</div>'
	document.body.append(d)
	const content = d.querySelector<HTMLElement>('.modal-content')!

	// close plays the exit fade, then closes for real; the timeout must outlast
	// the modal-out animation (website.css)
	let closeTimer = 0
	const close = () => {
		if (!d.open || closeTimer) return
		if (reducedMotion.matches) {
			d.close()
			return
		}
		d.classList.add('closing')
		closeTimer = setTimeout(() => {
			closeTimer = 0
			d.classList.remove('closing')
			d.close()
		}, 140)
	}

	const open = () => {
		// reopened mid-close (e.g. another trigger clicked during the exit fade):
		// abort the pending close and keep the dialog up
		clearTimeout(closeTimer)
		closeTimer = 0
		d.classList.remove('closing')
		// showModal doesn't lock background scroll; do it ourselves and restore
		// on close (covers the close button, scrim, and Escape)
		document.body.style.overflow = 'hidden'
		if (!d.open) d.showModal()
	}

	d.querySelector<HTMLButtonElement>('.modal-close')!.addEventListener('click', close)
	// the dialog element itself only shows through outside the box, so a click
	// targeting it is a scrim click
	d.addEventListener('click', (ev) => {
		if (ev.target === d) close()
	})
	// Escape: play the exit fade instead of the native immediate close
	d.addEventListener('cancel', (ev) => {
		ev.preventDefault()
		close()
	})
	d.addEventListener('close', () => {
		document.body.style.overflow = ''
	})

	return {dialog: d, content, open, close}
}
