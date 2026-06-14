import { EditorState, Compartment, Extension, Prec } from "@codemirror/state"
import { EditorView, keymap, placeholder } from "@codemirror/view"
import { bracketMatching } from "@codemirror/language"
import { defaultKeymap, history, historyKeymap } from "@codemirror/commands"
import { linter, Diagnostic } from "@codemirror/lint"
import { acceptCompletion, closeBrackets, closeBracketsKeymap } from "@codemirror/autocomplete"
import { tokyoNight } from "@uiw/codemirror-theme-tokyo-night"
import { tokyoNightDay } from "@uiw/codemirror-theme-tokyo-night-day"
import ottrecql from "./language"
import { ottrecqlCompletion, ottrecqlBadBrackets } from "./completion"

// makeDocNode renders the documentation popup shown beside a completion. The
// styling lives here with the rest of the component's presentation.
function makeDocNode(signature: string, doc: string, ...examples: string[]): Node {
    const el = document.createElement('div')
    el.style.cssText = 'padding:8px 12px;max-width:360px;font-size:13px;line-height:1.5'

    const sig = document.createElement('code')
    sig.style.cssText = 'display:block;font-size:12px;font-weight:600;margin-bottom:6px;white-space:pre-wrap;color:#4ec9b0'
    sig.textContent = signature
    el.appendChild(sig)

    const d = document.createElement('div')
    d.style.marginBottom = '8px'
    d.textContent = doc
    el.appendChild(d)

    if (examples.length) {
        const lbl = document.createElement('div')
        lbl.style.cssText = 'font-size:10px;text-transform:uppercase;letter-spacing:.07em;opacity:0.5;margin-bottom:3px'
        lbl.textContent = 'Examples'
        el.appendChild(lbl)
        for (const ex of examples) {
            const c = document.createElement('code')
            c.style.cssText = 'display:block;font-size:12px;color:#ce9178'
            c.textContent = ex
            el.appendChild(c)
        }
    }
    return el
}

// resolveTheme maps the theme attribute to a CodeMirror theme. Switching with
// the page colour scheme is left to whatever sets up the editor (it just sets
// the theme attribute); the component only knows the two Tokyo Night variants.
function resolveTheme(name: string | null): Extension {
    return name === 'tokyo-night' ? tokyoNight : tokyoNightDay
}

class OttrecqlEditor extends HTMLElement {
    static formAssociated = true
    static observedAttributes = ['value', 'theme', 'readonly', 'required', 'placeholder']

    #internals: ElementInternals
    #view: EditorView | null = null
    #editableCompartment = new Compartment()
    #themeCompartment = new Compartment()
    #lintCompartment = new Compartment()
    #placeholderCompartment = new Compartment()
    #dirty = false
    #lintUrl: string | null = null

    constructor() {
        super()
        this.#internals = this.attachInternals()
        this.attachShadow({ mode: 'open' })
        const style = document.createElement('style')
        style.textContent = `
          :host {
            display: block;
            overflow: hidden;
            cursor: text;
            border: 1px solid var(--fxki-ui-3, #888);
            border-radius: .35rem;
          }
          :host(:focus-within) {
            border-color: var(--fxki-bl, #4385be);
          }
          :host([disabled]) {
            opacity: 0.5;
            pointer-events: none;
          }
          :host([readonly]) {
            cursor: default;
          }
          .cm-editor {
            max-height: 240px;
          }
          .cm-scroller {
            overflow: auto !important;
          }
        `
        this.shadowRoot!.appendChild(style)
    }

    connectedCallback() {
        if (this.#view) return

        const container = document.createElement('div')
        this.shadowRoot!.appendChild(container)

        const initialValue = this.getAttribute('value') ?? ''
        const editable = !this.hasAttribute('disabled') && !this.hasAttribute('readonly')

        this.#view = new EditorView({
            root: this.shadowRoot!,
            state: EditorState.create({
                doc: initialValue,
                extensions: [
                    history(),
                    keymap.of([{ key: 'Tab', run: acceptCompletion }, ...closeBracketsKeymap, ...defaultKeymap, ...historyKeymap]),
                    bracketMatching(),
                    closeBrackets(),
                    ottrecql,
                    ottrecqlCompletion(makeDocNode),
                    ottrecqlBadBrackets(),
                    this.#placeholderCompartment.of(placeholder(this.getAttribute('placeholder') ?? '')),
                    this.#themeCompartment.of(resolveTheme(this.getAttribute('theme'))),
                    this.#editableCompartment.of(EditorView.editable.of(editable)),
                    this.#lintCompartment.of(this.#lintExtension()),
                    // show through to the host's background instead of the
                    // Tokyo Night theme's own blue-tinted one (the token
                    // colours still match light/dark since the host picks the
                    // matching variant)
                    Prec.highest(EditorView.theme({
                        '&': { backgroundColor: 'transparent' },
                        '.cm-gutters': { backgroundColor: 'transparent', borderRight: 'none' },
                        '&.cm-focused': { outline: 'none' },
                        '.cm-content': { padding: '8px 0' },
                        '.cm-line': { padding: '0 10px' },
                        '.cm-completionInfo': { padding: 0 },
                    })),
                    EditorView.lineWrapping,
                    EditorView.updateListener.of(update => {
                        if (update.docChanged) {
                            this.#dirty = true
                            const value = update.state.doc.toString()
                            this.#internals.setFormValue(value)
                            this.#validate(value)
                            this.dispatchEvent(new Event('input', { bubbles: true, composed: true }))
                        }
                        if (update.focusChanged && !update.view.hasFocus) {
                            this.dispatchEvent(new Event('change', { bubbles: true, composed: true }))
                        }
                    }),
                ],
            }),
            parent: container,
        })

        this.#internals.setFormValue(initialValue)
        this.#validate(initialValue)
    }

    disconnectedCallback() {
        this.#view?.destroy()
        this.#view = null
        this.shadowRoot!.querySelector('div')?.remove()
    }

    formResetCallback() {
        this.#dirty = false
        const defaultVal = this.getAttribute('value') ?? ''
        this.#setDoc(defaultVal)
        this.#internals.setFormValue(defaultVal)
        this.#validate(defaultVal)
    }

    formDisabledCallback(disabled: boolean) {
        this.#reconfigureEditable(disabled, this.hasAttribute('readonly'))
    }

    attributeChangedCallback(name: string, _oldVal: string | null, newVal: string | null) {
        if (name === 'theme') {
            this.#view?.dispatch({ effects: this.#themeCompartment.reconfigure(resolveTheme(newVal)) })
        }
        if (name === 'placeholder') {
            this.#view?.dispatch({ effects: this.#placeholderCompartment.reconfigure(placeholder(newVal ?? '')) })
        }
        if (name === 'readonly') {
            this.#reconfigureEditable(this.hasAttribute('disabled'), newVal !== null)
        }
        if (name === 'required') {
            this.#validate(this.value)
        }
        if (name === 'value' && !this.#dirty) {
            const val = newVal ?? ''
            this.#setDoc(val)
            this.#internals.setFormValue(val)
            this.#validate(val)
        }
    }

    #lintExtension(): Extension {
        if (!this.#lintUrl) return []
        const source = async (view: EditorView): Promise<Diagnostic[]> => {
            const url = this.#lintUrl
            if (!url) return []
            const len = view.state.doc.length
            const q = view.state.doc.toString()
            if (!q.trim()) return []
            let data: { error?: string, offset?: number }
            try {
                const resp = await fetch(url + '?q=' + encodeURIComponent(q))
                if (!resp.ok) return []
                data = await resp.json()
            } catch {
                return [] // don't get in the way if validation itself fails
            }
            if (!data.error) return []
            let from = typeof data.offset === 'number' ? Math.max(0, Math.min(data.offset, len)) : 0
            const to = len
            if (from >= to) from = Math.max(0, to - 1) // keep a visible range at end-of-input
            return [{ from, to, severity: 'error', message: data.error }]
        }
        return linter(source, { delay: 500 })
    }

    #setDoc(value: string) {
        this.#view?.dispatch({ changes: { from: 0, to: this.#view.state.doc.length, insert: value } })
    }

    #reconfigureEditable(disabled: boolean, readonly: boolean) {
        this.#view?.dispatch({
            effects: this.#editableCompartment.reconfigure(EditorView.editable.of(!disabled && !readonly)),
        })
    }

    #validate(value: string) {
        if (this.hasAttribute('required') && !value.trim()) {
            const anchor = this.#view?.contentDOM // setValidity requires an anchor
            if (anchor) this.#internals.setValidity({ valueMissing: true }, 'Please fill out this field.', anchor)
        } else {
            this.#internals.setValidity({})
        }
    }

    get value(): string {
        return this.#view?.state.doc.toString() ?? this.getAttribute('value') ?? ''
    }

    set value(v: string) {
        v = String(v ?? '')
        this.#setDoc(v)
        this.#internals.setFormValue(v)
        this.#validate(v)
    }

    // lint is the URL of a query-validation endpoint. It is set as a property
    // (rather than an attribute) so the page can wire its own endpoint without
    // coupling the component to a particular route. The endpoint receives the
    // query as ?q= and returns JSON {error?: string, offset?: number}.
    get lint(): string | null { return this.#lintUrl }
    set lint(v: string | null) {
        this.#lintUrl = v ? String(v) : null
        this.#view?.dispatch({ effects: this.#lintCompartment.reconfigure(this.#lintExtension()) })
    }

    get theme(): string { return this.getAttribute('theme') ?? 'auto' }
    set theme(v: string | null) {
        if (v === null || v === undefined || v === 'auto') this.removeAttribute('theme')
        else this.setAttribute('theme', v)
    }

    get defaultValue(): string { return this.getAttribute('value') ?? '' }
    set defaultValue(v: string) { this.setAttribute('value', String(v)) }

    get name(): string { return this.getAttribute('name') ?? '' }
    set name(v: string) { this.setAttribute('name', v) }

    get placeholder(): string { return this.getAttribute('placeholder') ?? '' }
    set placeholder(v: string) { this.setAttribute('placeholder', v) }

    get required(): boolean { return this.hasAttribute('required') }
    set required(v: boolean) { this.toggleAttribute('required', Boolean(v)) }

    get disabled(): boolean { return this.hasAttribute('disabled') }
    set disabled(v: boolean) { this.toggleAttribute('disabled', Boolean(v)) }

    get readOnly(): boolean { return this.hasAttribute('readonly') }
    set readOnly(v: boolean) { this.toggleAttribute('readonly', Boolean(v)) }

    get form(): HTMLFormElement | null { return this.#internals.form }
    get validity(): ValidityState { return this.#internals.validity }
    get validationMessage(): string { return this.#internals.validationMessage }
    get willValidate(): boolean { return this.#internals.willValidate }

    checkValidity(): boolean { return this.#internals.checkValidity() }
    reportValidity(): boolean { return this.#internals.reportValidity() }
}

customElements.define('ottrecql-editor', OttrecqlEditor)

export { OttrecqlEditor }

declare global {
    interface HTMLElementTagNameMap {
        'ottrecql-editor': OttrecqlEditor
    }
}
