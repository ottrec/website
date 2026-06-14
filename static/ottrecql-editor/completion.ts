import { StateField, RangeSetBuilder, EditorState } from "@codemirror/state"
import { EditorView, Decoration, DecorationSet } from "@codemirror/view"
import { autocompletion, Completion, CompletionSource } from "@codemirror/autocomplete"
import { Extension } from "@codemirror/state"
import { isFunction, WEEKDAYS } from "./language"

// DocNode renders the documentation shown beside a completion. It is provided
// by the caller so the styling can live with the component (see component.ts).
export type DocNode = (signature: string, doc: string, ...examples: string[]) => Node

// applyOperator completes an infix operator (and/or) with surrounding spaces,
// adding the leading space only when one isn't already there.
function applyOperator(op: string) {
    return (view: EditorView, _completion: Completion, from: number, to: number) => {
        const before = from > 0 ? view.state.doc.sliceString(from - 1, from) : ''
        const lead = from === 0 || /\s/.test(before) ? '' : ' '
        const insert = lead + op + ' '
        view.dispatch({
            changes: { from, to, insert },
            selection: { anchor: from + insert.length },
        })
    }
}

// getEnclosingFunction returns the name of the ottrecql function call directly
// containing pos, or null if pos is at the top level (not inside any call).
function getEnclosingFunction(state: EditorState, pos: number): string | null {
    const text = state.doc.sliceString(0, pos)
    let i = 0
    const stack: (string | null)[] = []
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
            stack.push(isFunction(name) ? name.toLowerCase() : null)
            i++; continue
        }
        if (text[i] === ')') { stack.pop(); i++; continue }
        i++
    }
    return stack.length > 0 ? stack[stack.length - 1]! : null
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

// createCompletions builds the context-keyed completion lists, rendering each
// option's documentation with the supplied DocNode.
function createCompletions(doc: DocNode): Record<string, Completion[]> {
    return {
        expr_start: [
            {
                label: 'schdate', type: 'function', apply: 'schdate(',
                info: () => doc('schdate(date)', 'Matches schedule groups applicable on the given date. Groups without a date range are not filtered.', 'schdate(today)', 'schdate(2025-12-24)')
            },
            {
                label: 'time', type: 'function', apply: 'time(',
                info: () => doc('time([weekday|date …] @ [time|range …])', 'Matches activity weekdays, dates, and/or times. The @ separator can be omitted when specifying only weekdays or only times. Activities with unparseable times are not filtered.', 'time(today @ now)', 'time(mo tu we th fr)', 'time(sa su @ 18:00-01:00)', 'time(mo @ 6:00a-10:00a 6:00p-9:00p)')
            },
            {
                label: 'facility', type: 'function', apply: 'facility(',
                info: () => doc('facility("name" [, "name" …])', 'Fuzzy-matches facility names. Each word of the query must prefix a word in the name, in order.', 'facility("splash")', 'facility("st laurent")', 'facility("tom brown", "jim durrell")')
            },
            {
                label: 'activity', type: 'function', apply: 'activity(',
                info: () => doc('activity("name" [, "name" …])', 'Like facility(), but matches activity names. Generally uses the infinitive form (e.g. "skate" not "skating").', 'activity("lane swim")', 'activity("figure skate")', 'activity("lane swim", "public swim")')
            },
            {
                label: 'latlng', type: 'function', apply: 'latlng(',
                info: () => doc('latlng(lat, lng, km)', 'Matches facilities within a radius (km) of the given coordinates.', 'latlng(45.42620, -75.69205, 2)')
            },
            {
                label: 'not', type: 'keyword',
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

// ottrecqlCompletion provides context-aware autocompletion: function names and
// operators at the top level, and the keywords valid inside each function.
export function ottrecqlCompletion(doc: DocNode): Extension {
    const completions = createCompletions(doc)
    const source: CompletionSource = context => {
        const word = context.matchBefore(/[a-zA-Z][a-zA-Z0-9]*/)
        const from = word ? word.from : context.pos
        const ctx = getEnclosingFunction(context.state, from)
        const key = ctx === null
            ? (atExpressionStart(context.state, from) ? 'expr_start' : 'expr_continue')
            : ctx
        const options = completions[key] ?? []
        if (!options.length) return null
        if (!word && !context.explicit && !context.matchBefore(/[\s!()/]/)) return null
        return { from, options, validFor: /^[a-zA-Z0-9]*$/ }
    }
    return autocompletion({ override: [source] })
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
