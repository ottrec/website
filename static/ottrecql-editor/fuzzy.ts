// Fuzzy name completion for facility() and activity() string arguments. This
// mirrors the server's fuzzy word matcher (pkg/ottrecql/util.go): names and
// queries are normalized by mapping dashes to spaces, stripping diacritics and
// periods, and lowercasing; a query matches when each of its words is a prefix
// of a name word, in order. Here we use that to suggest the next word (or, once
// a single name remains, its whole tail) given what's been typed so far.

// normalize folds a string the way the server matcher does: dashes → spaces,
// diacritics removed, periods dropped, lowercased.
export function normalize(s: string): string {
    return s
        .normalize('NFKD')
        .replace(/\p{Mn}/gu, '')        // strip accents (nonspacing marks)
        .replace(/[\p{Pd}]/gu, ' ')     // all dashes → space
        .replace(/\./g, '')             // drop periods
        .toLowerCase()
}

// NormalizedName is a name split into its normalized words.
export type NormalizedName = string[]

// toNormalizedName splits a raw name into normalized words.
export function toNormalizedName(name: string): NormalizedName {
    return normalize(name).split(/\s+/).filter(Boolean)
}

// NameSuggestion describes one completion for a facility/activity string. The
// replacement runs from `fromOffset` (relative to the string's content start)
// to the cursor; all suggestions from a single call share the same fromOffset.
export type NameSuggestion = {
    fromOffset: number  // where to start replacing, relative to the string content start
    insert: string      // normalized text to insert (without quotes)
    label: string       // what to show in the list
    complete: boolean   // the insert finishes a whole name (so close the string)
}

// matchSubsequence matches the committed query words against a name as an
// in-order subsequence (each word a prefix of a name word, skipping allowed,
// like the server), returning the name index where the last committed word
// matched (-1 when there are none), or null if they can't all be matched.
function matchSubsequence(name: NormalizedName, committed: string[]): number | null {
    let idx = -1
    for (const cw of committed) {
        let found = -1
        for (let j = idx + 1; j < name.length; j++) {
            if (name[j]!.startsWith(cw)) { found = j; break }
        }
        if (found === -1) return null
        idx = found
    }
    return idx
}

// MAX_SUGGESTIONS caps the next-word list, which can otherwise be large (e.g. an
// empty partial matches every word). The source re-queries as the user types.
const MAX_SUGGESTIONS = 50

// suggestNames returns completions for the cursor position inside a quoted
// argument, given the raw text typed from the opening quote to the cursor and
// the candidate names. The committed words match as an in-order subsequence
// (words may be skipped, like the server matcher), then a later word prefixed by
// the partial is the candidate. When exactly one name remains, the rest of it is
// filled in forward (never prepending skipped words); otherwise the distinct
// next words are offered. All replacements run from the partial word, so words
// the user already typed are left as written.
export function suggestNames(content: string, names: readonly NormalizedName[]): NameSuggestion[] {
    const norm = normalize(content)
    const trailingSpace = content === '' || /\s$/.test(content)
    const words = norm.split(/\s+/).filter(Boolean)

    const committed = trailingSpace ? words : words.slice(0, -1)
    const partial = trailingSpace ? '' : (words[words.length - 1] ?? '')

    // offset of the partial word within the raw content (start of the run of
    // non-space characters at the end, or the cursor when none)
    const partialRaw = trailingSpace ? '' : (content.match(/\S*$/)?.[0] ?? '')
    const partialOffset = content.length - partialRaw.length

    // viable names: committed matched as a subsequence, with at least one later
    // word the partial can prefix
    const viable: { name: NormalizedName, lastIdx: number, firstJ: number }[] = []
    for (const name of names) {
        const lastIdx = matchSubsequence(name, committed)
        if (lastIdx === null) continue
        let firstJ = -1
        for (let j = lastIdx + 1; j < name.length; j++) {
            if (name[j]!.startsWith(partial)) { firstJ = j; break }
        }
        if (firstJ === -1) continue
        viable.push({ name, lastIdx, firstJ })
    }
    if (viable.length === 0) return []

    // a single candidate: fill in the rest of the name from the matched word
    if (viable.length === 1) {
        const { name, firstJ } = viable[0]!
        const tail = name.slice(firstJ).join(' ')
        return [{ fromOffset: partialOffset, insert: tail, label: tail, complete: true }]
    }

    // otherwise: the distinct next words across every viable candidate position.
    // A word that ends every name it appears in here closes the string; one that
    // can continue keeps it open and re-opens completion for the next word.
    const terminalByWord = new Map<string, boolean>()
    for (const { name, lastIdx } of viable) {
        for (let j = lastIdx + 1; j < name.length; j++) {
            if (!name[j]!.startsWith(partial)) continue
            const w = name[j]!
            const terminal = j === name.length - 1
            const prev = terminalByWord.get(w)
            terminalByWord.set(w, prev === undefined ? terminal : prev && terminal)
        }
    }
    const out: NameSuggestion[] = [...terminalByWord].map(([w, complete]) => (
        { fromOffset: partialOffset, insert: w, label: w, complete }
    ))
    out.sort((a, b) => a.label.localeCompare(b.label))
    return out.slice(0, MAX_SUGGESTIONS)
}
