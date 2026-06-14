import { StreamLanguage, LanguageSupport, StreamParser, StringStream } from "@codemirror/language"
import { tags } from "@lezer/highlight"

export const WEEKDAYS = [
    'sunday', 'monday', 'tuesday', 'wednesday', 'thursday', 'friday', 'saturday',
    'sun', 'mon', 'tue', 'wed', 'thu', 'fri', 'sat',
    'su', 'mo', 'tu', 'we', 'th', 'fr', 'sa',
] as const

export const FUNCTIONS = [
    'schdate',
    'time',
    'facility',
    'activity',
    'latlng',
] as const

export const isWeekday = createMatcher(...WEEKDAYS)
export const isFunction = createMatcher(...FUNCTIONS)

const eatTime = createEater(/\d{1,2}:\d{2}(?:[aApP][mM]?)?(?![a-zA-Z0-9])/)
const eatDate = createEater(/\d{4}-\d{2}-\d{2}/)
const eatNumber = createEater(/\d+(?:\.\d+)?/)
const eatIdent = createEater(/[a-zA-Z][a-zA-Z0-9]*/)

export const parser: StreamParser<void> = {
    name: "query",

    token(stream) {
        if (stream.eatSpace()) {
            return null
        }
        const ch = stream.peek()
        if (ch) {
            // string
            if (ch === '"') {
                stream.next()
                while (!stream.eol()) {
                    const c = stream.next()
                    if (c === "\\") { stream.next(); continue }
                    if (c === '"') break
                }
                return "qStr"
            }

            // two-character operators
            if (stream.match("&&") || stream.match("||")) return "qOp"

            // single-character symbolic operators and delimiters
            if (stream.match("!")) return "qOp"
            if (stream.match("(") || stream.match(")")) return "qParen"
            if (stream.match(",") || stream.match("@") || stream.match("-")) return "qPunct"

            // date, time, or number
            if (ch >= "0" && ch <= "9") {
                if (eatDate(stream)) return "qDate"
                if (eatTime(stream)) return "qTime"
                if (eatNumber(stream)) return "qNum"
            }

            // keywords and functions
            if ((ch >= "a" && ch <= "z") || (ch >= "A" && ch <= "Z")) {
                if (eatIdent(stream)) {
                    const word = stream.current()
                    if (word === "not" || word === "and" || word === "or") return "qOp"
                    if (word === "today") return "qDate"
                    if (word === "now") return "qTime"
                    if (isWeekday(word)) return "qWeekday"
                    if (isFunction(word)) return "qFunc"
                    return null
                }
            }
        }
        stream.next()
        return null
    },

    tokenTable: {
        qStr: tags.string,
        qOp: tags.operator,
        qParen: tags.paren,
        qPunct: tags.punctuation,
        qDate: tags.special(tags.number),
        qTime: tags.special(tags.string),
        qNum: tags.number,
        qWeekday: tags.atom,
        qFunc: tags.function(tags.variableName),
    },
} as const

const language = StreamLanguage.define(parser)

export default new LanguageSupport(language, [
    // only ottrecql's own delimiters get auto-closed (not the closeBrackets
    // defaults like [] {} '' ``, which aren't valid syntax here)
    language.data.of({ closeBrackets: { brackets: ['(', '"'] } }),
])

function createMatcher(...values: string[]) {
    const set = new Set(values.map(v => v.toLowerCase()))
    return function is(value: string): boolean {
        return set.has(value.toLowerCase())
    }
}


function createEater(re: RegExp) {
  re = new RegExp(re.source, `${re.flags}${re.sticky ? '' : 'y'}`)
  return function eat(stream: StringStream): boolean {
    re.lastIndex = stream.pos
    re.test(stream.string)
    if (re.lastIndex > stream.pos) {
        while (stream.pos < re.lastIndex) {
            stream.next()
        }
        return true
    }
    return false
  }
}
