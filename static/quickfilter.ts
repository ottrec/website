// Fuzzy quick-filter matching shared by the /today feed and the activity
// landing page's today widget: normalize + stem + typo-tolerant token matching.

import {normalizeText} from './text'

// stem crudely de-suffixes a word ("skating" and "skate" both become "skat",
// "classes" becomes "class") and folds "mac" onto "mc", so common inflections
// and name variants compare equal. English-biased, but harmless elsewhere
// since both sides are stemmed the same way.
export function stem(w: string): string {
	if (w.startsWith('mac')) w = 'mc' + w.slice(3)
	const undouble = (v: string) => (v.length > 2 && v[v.length - 1] === v[v.length - 2] ? v.slice(0, -1) : v) // swimm -> swim
	if (w.length > 5 && w.endsWith('ing')) w = undouble(w.slice(0, -3))
	else if (w.length > 6 && w.endsWith('ers')) w = undouble(w.slice(0, -3))
	else if (w.length > 5 && w.endsWith('er')) w = undouble(w.slice(0, -2))
	else if (w.length > 4 && w.endsWith('es')) w = w.slice(0, -2)
	else if (w.length > 3 && w.endsWith('s') && !w.endsWith('ss')) w = w.slice(0, -1)
	if (w.length > 3 && w.endsWith('e')) w = w.slice(0, -1)
	return w
}

// quickNorm normalizes text for quick-filter matching: on top of normalizeText,
// punctuation collapses to spaces so "st laurent" matches "St-Laurent".
export function quickNorm(s: string): string {
	return normalizeText(s).replace(/[^\p{L}\p{N}]+/gu, ' ').trim()
}

// editDist is the Levenshtein distance between a and b, giving up (returning
// max+1) once it exceeds max.
function editDist(a: string, b: string, max: number): number {
	if (Math.abs(a.length - b.length) > max) return max + 1
	let prev = Array.from({length: b.length + 1}, (_, i) => i)
	let cur = new Array<number>(b.length + 1)
	for (let i = 1; i <= a.length; i++) {
		cur[0] = i
		let best = i
		for (let j = 1; j <= b.length; j++) {
			cur[j] = Math.min(prev[j]! + 1, cur[j - 1]! + 1, prev[j - 1]! + (a[i - 1] === b[j - 1] ? 0 : 1))
			best = Math.min(best, cur[j]!)
		}
		if (best > max) return max + 1
		;[prev, cur] = [cur, prev]
	}
	return prev[b.length]!
}

// a quick-filter query word, its stem, and the typo tolerance each earns from
// its length
export interface QuickToken {
	t: string
	stem: string
	k: number
	ks: number
}

export function quickTokens(q: string): QuickToken[] {
	const fuzz = (n: number) => (n >= 7 ? 2 : n >= 4 ? 1 : 0)
	return quickNorm(q).split(' ').filter(Boolean).map((t) => {
		const st = stem(t)
		return {t, stem: st, k: fuzz(t.length), ks: fuzz(st.length)}
	})
}

// QuickTarget is the precomputed searchable form of a row (its normalized text,
// the same with spaces dropped, and per-word stems).
export interface QuickTarget {
	text: string
	compact: string
	words: string[]
	stems: string[]
}

// quickTarget builds a QuickTarget from raw text (e.g. "activity facility").
export function quickTarget(raw: string): QuickTarget {
	const text = quickNorm(raw)
	const words = text.split(' ')
	return {text, compact: text.replaceAll(' ', ''), words, stems: words.map(stem)}
}

// a token matches a target by substring (against the spaced and space-dropped
// text, so partial words and "aqua fit"/"aquafit" both work), by stem prefix
// ("skating" matches "skate", "mcquarrie" matches "MacQuarrie"), or fuzzily
// against each word, same-length word prefix, or stem (so typos still match,
// including in a partially typed word).
function quickTokenMatches(s: QuickTarget, tok: QuickToken): boolean {
	if (s.text.includes(tok.t) || s.compact.includes(tok.t)) return true
	for (let i = 0; i < s.words.length; i++) {
		if (s.stems[i]!.startsWith(tok.stem)) return true
		if (!tok.k) continue
		const w = s.words[i]!
		if (editDist(tok.t, w, tok.k) <= tok.k) return true
		if (w.length > tok.t.length && editDist(tok.t, w.slice(0, tok.t.length), tok.k) <= tok.k) return true
		if (tok.ks && editDist(tok.stem, s.stems[i]!, tok.ks) <= tok.ks) return true
	}
	return false
}

// quickMatch reports whether a target matches every query token (AND).
export function quickMatch(s: QuickTarget, tokens: QuickToken[]): boolean {
	return tokens.every((tok) => quickTokenMatches(s, tok))
}
