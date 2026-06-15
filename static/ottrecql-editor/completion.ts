import { StateField, StateEffect, RangeSetBuilder, EditorState } from "@codemirror/state"
import { EditorView, Decoration, DecorationSet, showTooltip, Tooltip } from "@codemirror/view"
import { autocompletion, startCompletion, completionStatus, Completion, CompletionSource } from "@codemirror/autocomplete"
import { Extension } from "@codemirror/state"
import { isFunction, WEEKDAYS } from "./language"
import { suggestNames, NormalizedName } from "./fuzzy"

// NameLists supplies the candidate names for facility()/activity() string
// completion; the component fills these in once fetched (see component.ts).
export type NameLists = { facility: readonly NormalizedName[], activity: readonly NormalizedName[] }

// DocNode renders the documentation shown beside a completion. It is provided
// by the caller so the styling can live with the component (see component.ts).
export type DocNode = (signature: string, doc: string, ...examples: string[]) => HTMLElement

// applyOperator completes an operator (and/or/not) with surrounding spaces,
// adding the leading space only when one isn't already there, then reopens the
// completion popup so the next expression's options show right away.
function applyOperator(op: string) {
    return (view: EditorView, _completion: Completion, from: number, to: number) => {
        const before = from > 0 ? view.state.doc.sliceString(from - 1, from) : ''
        const lead = from === 0 || /\s/.test(before) ? '' : ' '
        const insert = lead + op + ' '
        view.dispatch({
            changes: { from, to, insert },
            selection: { anchor: from + insert.length },
        })
        startCompletion(view)
    }
}

// EnclosingCall describes the parenthesised call directly containing a position:
// the function name (lowercased, or null for a non-function grouping) and the
// document offset where that name starts (the tooltip's anchor).
type EnclosingCall = { name: string | null, from: number }

// getEnclosingFunction returns the call directly containing pos, or null if pos
// is at the top level (not inside any parentheses).
function getEnclosingFunction(state: EditorState, pos: number): EnclosingCall | null {
    const text = state.doc.sliceString(0, pos)
    let i = 0
    const stack: EnclosingCall[] = []
    while (i < text.length) {
        if (text[i] === '"') {
            i++
            while (i < text.length) {
                if (text[i] === '\\') { i += 2; continue }
                if (text[i] === '"') { i++; break }
                i++
            }
            continue
        }
        if (text[i] === '(') {
            let j = i - 1
            while (j >= 0 && /\s/.test(text[j]!)) j--
            let k = j
            while (k >= 0 && /[a-zA-Z0-9]/.test(text[k]!)) k--
            const name = text.slice(k + 1, j + 1)
            stack.push({ name: isFunction(name) ? name.toLowerCase() : null, from: k + 1 })
            i++; continue
        }
        if (text[i] === ')') { stack.pop(); i++; continue }
        i++
    }
    return stack.length > 0 ? stack[stack.length - 1]! : null
}

// stringContentStart returns the document offset just after the opening quote of
// the string containing pos, or null if pos is not inside a string.
function stringContentStart(state: EditorState, pos: number): number | null {
    const text = state.doc.sliceString(0, pos)
    let i = 0, start = -1, inString = false
    while (i < text.length) {
        if (inString) {
            if (text[i] === '\\') { i += 2; continue }
            if (text[i] === '"') { inString = false; i++; continue }
            i++
        } else {
            if (text[i] === '"') { inString = true; start = i + 1; i++; continue }
            i++
        }
    }
    return inString ? start : null
}

// atExpressionStart reports whether pos is where a new sub-expression may begin
// (the start of the input, after '(' or '!', or after a logical operator).
function atExpressionStart(state: EditorState, pos: number): boolean {
    const text = state.doc.sliceString(0, pos)
    let i = text.length - 1
    while (i >= 0 && /\s/.test(text[i]!)) i--
    if (i < 0) return true // beginning
    const ch = text[i]!
    if (ch === '(' || ch === '!') return true // beginning of subexpression
    if ((ch === '&' || ch === '|') && i > 0 && text[i - 1] === ch) return true // after operator
    if (/[a-zA-Z]/.test(ch)) {
        let j = i
        while (j >= 0 && /[a-zA-Z]/.test(text[j]!)) j--
        const kw = text.slice(j + 1, i + 1).toLowerCase()
        return kw === 'not' || kw === 'and' || kw === 'or' // after operator
    }
    return false
}

// FUNCTION_DOCS is the single source of documentation for each match function,
// shared by the completion popup (signature + doc + examples) and the
// signature-help tooltip (signature + doc only). Ordered as shown in the popup.
const FUNCTION_DOCS: Record<string, { signature: string, doc: string, examples: string[] }> = {
    schdate: {
        signature: 'schdate(date)',
        doc: 'Matches schedule groups applicable on the given date. Groups without a date range are not filtered.',
        examples: ['schdate(today)', 'schdate(2025-12-24)'],
    },
    time: {
        signature: 'time([weekday|date …] @ [time|range …])',
        doc: 'Matches activity weekdays, dates, and/or times. The @ separator can be omitted when specifying only weekdays or only times. Activities with unparseable times are not filtered.',
        examples: ['time(today @ now)', 'time(mo tu we th fr)', 'time(sa su @ 18:00-01:00)', 'time(mo @ 6:00a-10:00a 6:00p-9:00p)'],
    },
    facility: {
        signature: 'facility("name" [, "name" …])',
        doc: 'Fuzzy-matches facility names. Each word of the query must prefix a word in the name, in order.',
        examples: ['facility("splash")', 'facility("st laurent")', 'facility("tom brown", "jim durrell")'],
    },
    activity: {
        signature: 'activity("name" [, "name" …])',
        doc: 'Like facility(), but matches activity names. Generally uses the infinitive form (e.g. "skate" not "skating").',
        examples: ['activity("lane swim")', 'activity("figure skate")', 'activity("lane swim", "public swim")'],
    },
    latlng: {
        signature: 'latlng(lat, lng, km)',
        doc: 'Matches facilities within a radius (km) of the given coordinates.',
        examples: ['latlng(45.42620, -75.69205, 2)'],
    },
}

// createCompletions builds the context-keyed completion lists, rendering each
// option's documentation with the supplied DocNode.
function createCompletions(doc: DocNode): Record<string, Completion[]> {
    return {
        expr_start: [
            ...Object.entries(FUNCTION_DOCS).map(([name, d]): Completion => ({
                label: name, type: 'function', apply: name + '(',
                info: () => doc(d.signature, d.doc, ...d.examples),
            })),
            {
                label: 'not', type: 'keyword', apply: applyOperator('not'),
                info: () => doc('not expr  |  !expr', 'Logical NOT. Excludes results that match the expression.', 'not activity("adult")', '!facility("plant")')
            },
            {
                label: '(', type: 'syntax', apply: '(',
                info: () => doc('(expr)', 'Groups an expression to override operator precedence.', '(schdate(today) or schdate(2025-12-24)) and time(Mon @ 9:00)', 'not (facility("A") or facility("B"))')
            },
        ],
        expr_continue: [
            {
                label: 'and', type: 'keyword', apply: applyOperator('and'),
                info: () => doc('expr and expr  |  expr && expr', 'Logical AND. Both expressions must match.', 'schdate(today) and activity("lane swim")')
            },
            {
                label: 'or', type: 'keyword', apply: applyOperator('or'),
                info: () => doc('expr or expr  |  expr || expr', 'Logical OR. Either expression must match.', 'schdate(today) or schdate(2025-12-24)')
            },
        ],
        schdate: [
            {
                label: 'today', type: 'keyword',
                info: () => doc('today', 'The current date (America/Toronto timezone).', 'schdate(today)')
            },
        ],
        time: [
            {
                label: 'today', type: 'keyword',
                info: () => doc('today', 'Matches activities on today\'s weekday at any time.', 'time(today)', 'time(today @ now)')
            },
            {
                label: 'now', type: 'keyword',
                info: () => doc('now', 'Current clock time (America/Toronto timezone). No date is implied.', 'time(now)', 'time(today @ now)')
            },
            ...WEEKDAYS.map((label): Completion => ({
                label, type: 'keyword',
                info: () => doc('Mo | Mon | Monday', 'Weekday specifier for use in time(). Accepts two-letter, three-letter, or full-name forms (case-insensitive).', 'time(mo tu we th fr)', 'time(sa su @ 18:00-21:00)')
            })),
        ],
    }
}

// ottrecqlCompletion provides context-aware autocompletion: facility/activity
// name words inside string arguments (from getNames), function names and
// operators at the top level, and the keywords valid inside each function.
export function ottrecqlCompletion(doc: DocNode, getNames: () => NameLists): Extension {
    const completions = createCompletions(doc)
    const source: CompletionSource = context => {
        // inside a facility()/activity() string, complete from the name list
        const enc = getEnclosingFunction(context.state, context.pos)
        if (enc && (enc.name === 'facility' || enc.name === 'activity')) {
            const contentStart = stringContentStart(context.state, context.pos)
            if (contentStart === null) return null
            const names = enc.name === 'facility' ? getNames().facility : getNames().activity
            if (!names.length) return null
            const content = context.state.doc.sliceString(contentStart, context.pos)
            const suggestions = suggestNames(content, names)
            if (!suggestions.length) return null
            const options: Completion[] = suggestions.map(s => ({
                label: s.label,
                type: 'text',
                apply: (view, _c, from, to) => {
                    if (s.complete) {
                        // finish the string; step over an auto-closed quote if
                        // one already follows, otherwise add the closing quote
                        const hasQuote = view.state.sliceDoc(to, to + 1) === '"'
                        const insert = hasQuote ? s.insert : s.insert + '"'
                        view.dispatch({
                            changes: { from, to, insert },
                            selection: { anchor: from + insert.length + (hasQuote ? 1 : 0) },
                        })
                    } else {
                        // insert just the word, no trailing space: the user
                        // decides whether to stop at this prefix or type a space
                        // to continue (which nudges toward minimal queries). The
                        // next word is suggested once they type that space.
                        view.dispatch({
                            changes: { from, to, insert: s.insert },
                            selection: { anchor: from + s.insert.length },
                        })
                    }
                },
            }))
            return { from: contentStart + suggestions[0]!.fromOffset, options }
        }

        const word = context.matchBefore(/[a-zA-Z][a-zA-Z0-9]*/)
        const from = word ? word.from : context.pos
        const key = enc?.name ?? (atExpressionStart(context.state, from) ? 'expr_start' : 'expr_continue')
        const options = completions[key] ?? []
        if (!options.length) return null
        if (!word && !context.explicit && !context.matchBefore(/[\s!()/]/)) return null
        return { from, options, validFor: /^[a-zA-Z0-9]*$/ }
    }
    return [
        autocompletion({ override: [source] }),
        // opening completions on focus surfaces the options for the current
        // position (the source returns null when none apply). Deferred a frame
        // so a click-to-focus has placed the cursor before we compute them.
        EditorView.domEventHandlers({
            focus: (_e, view) => {
                requestAnimationFrame(() => { if (view.hasFocus) startCompletion(view) })
                return false
            },
        }),
        // after the user types a space inside a facility()/activity() string,
        // suggest the next word (completions don't auto-advance there, so the
        // user can stop at a minimal prefix)
        EditorView.updateListener.of(update => {
            if (!update.docChanged) return
            if (!update.transactions.some(tr => tr.isUserEvent('input.type'))) return
            const pos = update.state.selection.main.head
            if (update.state.sliceDoc(pos - 1, pos) !== ' ') return
            const enc = getEnclosingFunction(update.state, pos)
            if (enc && (enc.name === 'facility' || enc.name === 'activity') && stringContentStart(update.state, pos) !== null) {
                const view = update.view
                requestAnimationFrame(() => { if (view.hasFocus) startCompletion(view) })
            }
        }),
    ]
}

// setFocused carries the editor's focus state into editor state, so the
// signature-help field (which is computed from state) can gate on it.
const setFocused = StateEffect.define<boolean>()

// signatureFor returns the tooltip(s) for the function enclosing the cursor,
// plus a key identifying them so the field can reuse a stable tooltip object
// (avoiding flicker/repositioning while typing within the same call). Nothing
// shows unless the editor is focused, and it yields to the completion popup
// (which already renders the same docs beside the active option).
function signatureFor(state: EditorState, focused: boolean, doc: DocNode): { key: string, tooltips: readonly Tooltip[] } {
    if (focused && completionStatus(state) !== 'active') {
        const enc = getEnclosingFunction(state, state.selection.main.head)
        const d = enc?.name ? FUNCTION_DOCS[enc.name] : undefined
        if (enc && d) {
            return {
                key: `${enc.name}@${enc.from}`,
                tooltips: [{ pos: enc.from, above: true, create: () => ({ dom: doc(d.signature, d.doc) }) }],
            }
        }
    }
    return { key: '', tooltips: [] }
}

// ottrecqlSignatureHelp shows the enclosing function's signature and description
// in a tooltip above the call while the editor is focused, so the docs stay
// visible as you fill in arguments (not just when a completion is highlighted).
export function ottrecqlSignatureHelp(doc: DocNode): Extension {
    const field = StateField.define<{ focused: boolean, key: string, tooltips: readonly Tooltip[] }>({
        create: () => ({ focused: false, key: '', tooltips: [] }),
        update(value, tr) {
            let focused = value.focused
            for (const e of tr.effects) if (e.is(setFocused)) focused = e.value
            const next = signatureFor(tr.state, focused, doc)
            if (next.key === value.key) {
                return focused === value.focused ? value : { ...value, focused }
            }
            return { focused, key: next.key, tooltips: next.tooltips }
        },
        provide: f => showTooltip.computeN([f], state => state.field(f).tooltips),
    })
    return [
        field,
        EditorView.domEventHandlers({
            focus: (_e, view) => { view.dispatch({ effects: setFocused.of(true) }); return false },
            blur: (_e, view) => { view.dispatch({ effects: setFocused.of(false) }); return false },
        }),
    ]
}

const badBracketMark = Decoration.mark({ class: 'cm-bad-bracket' })

// computeBadBrackets marks every unmatched '(' or ')' outside of strings.
function computeBadBrackets(state: EditorState): DecorationSet {
    const doc = state.doc.toString()
    const stack: number[] = []
    const bad: number[] = []
    let i = 0
    while (i < doc.length) {
        if (doc[i] === '"') {
            i++
            while (i < doc.length) {
                if (doc[i] === '\\') { i += 2; continue }
                if (doc[i] === '"') { i++; break }
                i++
            }
            continue
        }
        if (doc[i] === '(') { stack.push(i); i++; continue }
        if (doc[i] === ')') {
            if (stack.length > 0) stack.pop()
            else bad.push(i)
            i++; continue
        }
        i++
    }
    bad.push(...stack)
    bad.sort((a, b) => a - b)
    const builder = new RangeSetBuilder<Decoration>()
    for (const pos of bad) builder.add(pos, pos + 1, badBracketMark)
    return builder.finish()
}

const badBracketField = StateField.define<DecorationSet>({
    create: state => computeBadBrackets(state),
    update: (decos, tr) => tr.docChanged ? computeBadBrackets(tr.state) : decos,
    provide: f => EditorView.decorations.from(f),
})

// ottrecqlBadBrackets underlines unbalanced parentheses as you type.
export function ottrecqlBadBrackets(): Extension {
    return [
        badBracketField,
        EditorView.baseTheme({
            '.cm-bad-bracket': {
                backgroundColor: 'rgba(255, 80, 80, 0.2)',
                borderBottom: '2px solid rgba(255, 80, 80, 0.8)',
            },
        }),
    ]
}
