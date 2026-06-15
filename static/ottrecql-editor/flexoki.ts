import { Extension } from "@codemirror/state"
import { EditorView } from "@codemirror/view"
import { HighlightStyle, syntaxHighlighting } from "@codemirror/language"
import { tags as t } from "@lezer/highlight"

// Flexoki (https://stephango.com/flexoki) adapted to a CodeMirror 6 theme. The
// colours and token mapping mirror the official VS Code theme (vscode/
// Flexoki-{Dark,Light}-color-theme.json): dark uses the 400-step accents on the
// near-black "base" backgrounds, light uses the 600-step accents on the paper
// backgrounds. The structure follows the @uiw createTheme themes the component
// previously used (a base EditorView.theme plus a syntaxHighlighting style), so
// it drops in the same way.

interface Palette {
    dark: boolean
    // base / interface tones, dark→light ramp
    bg: string        // editor background (base-black / paper)
    bg2: string       // raised surfaces, line highlight
    ui: string        // borders, selected option
    ui2: string       // hover, brighter border
    ui3: string       // active selection
    tx3: string       // faint text (gutter numbers)
    tx2: string       // muted text (comments, punctuation)
    tx: string        // primary text
    // accents
    red: string
    orange: string
    yellow: string
    green: string
    cyan: string
    blue: string
    purple: string
    magenta: string
    selection: string // translucent selection fill
}

const dark: Palette = {
    dark: true,
    bg: "#100F0F",
    bg2: "#1C1B1A",
    ui: "#282726",
    ui2: "#343331",
    ui3: "#403E3C",
    tx3: "#575653",
    tx2: "#878580",
    tx: "#CECDC3",
    red: "#D14D41",
    orange: "#DA702C",
    yellow: "#D0A215",
    green: "#879A39",
    cyan: "#3AA99F",
    blue: "#4385BE",
    purple: "#8B7EC8",
    magenta: "#CE5D97",
    selection: "#CECDC333",
}

const light: Palette = {
    dark: false,
    bg: "#FFFCF0",
    bg2: "#F2F0E5",
    ui: "#E6E4D9",
    ui2: "#DAD8CE",
    ui3: "#CECDC3",
    tx3: "#B7B5AC",
    tx2: "#6F6E69",
    tx: "#100F0F",
    red: "#AF3029",
    orange: "#BC5215",
    yellow: "#AD8301",
    green: "#66800B",
    cyan: "#24837B",
    blue: "#205EA6",
    purple: "#5E409D",
    magenta: "#A02F6F",
    selection: "#100F0F44",
}

function makeTheme(p: Palette): Extension {
    const theme = EditorView.theme({
        "&": {
            color: p.tx,
            backgroundColor: p.bg,
        },
        ".cm-content": {
            caretColor: p.tx,
        },
        ".cm-cursor, .cm-dropCursor": {
            borderLeftColor: p.tx,
        },
        "&.cm-focused .cm-selectionBackground, & .cm-line::selection, & .cm-selectionLayer .cm-selectionBackground, .cm-content ::selection": {
            background: p.selection + " !important",
        },
        "& .cm-selectionMatch": {
            backgroundColor: p.ui2,
        },
        "&.cm-focused .cm-matchingBracket, &.cm-focused .cm-nonmatchingBracket": {
            backgroundColor: p.ui,
            outline: `1px solid ${p.ui2}`,
        },
        ".cm-gutters": {
            backgroundColor: p.bg,
            color: p.tx3,
            borderRightColor: "transparent",
        },
        ".cm-activeLine": {
            backgroundColor: p.bg2,
        },
        ".cm-activeLineGutter": {
            backgroundColor: p.bg2,
            color: p.tx,
        },
        // completion / tooltip surfaces, using the interface tones
        ".cm-tooltip": {
            backgroundColor: p.bg2,
            border: `1px solid ${p.ui2}`,
            color: p.tx,
        },
        ".cm-tooltip.cm-tooltip-autocomplete > ul > li[aria-selected]": {
            backgroundColor: p.ui2,
            color: p.tx,
        },
        ".cm-tooltip-autocomplete ul li[aria-selected] .cm-completionDetail": {
            color: p.tx2,
        },
        ".cm-completionDetail": {
            color: p.tx2,
        },
        ".cm-completionMatchedText": {
            color: p.blue,
            textDecoration: "none",
        },
    }, { dark: p.dark })

    const highlight = HighlightStyle.define([
        { tag: t.comment, color: p.tx2, fontStyle: "italic" },
        { tag: t.lineComment, color: p.tx2, fontStyle: "italic" },
        { tag: t.blockComment, color: p.tx2, fontStyle: "italic" },
        { tag: t.docComment, color: p.tx3, fontStyle: "italic" },
        { tag: [t.name, t.deleted, t.character, t.macroName], color: p.tx },
        { tag: t.variableName, color: p.tx },
        { tag: [t.propertyName], color: p.blue },
        { tag: [t.processingInstruction, t.string, t.inserted, t.special(t.string)], color: p.cyan },
        { tag: t.escape, color: p.tx },
        { tag: [t.function(t.variableName), t.function(t.propertyName)], color: p.orange, fontWeight: "bold" },
        { tag: t.labelName, color: p.magenta },
        { tag: [t.color, t.constant(t.name), t.standard(t.name)], color: p.tx },
        { tag: [t.definition(t.name), t.separator], color: p.tx },
        { tag: [t.className], color: p.orange },
        { tag: [t.number, t.special(t.number), t.integer, t.float], color: p.purple },
        { tag: [t.changed, t.annotation, t.modifier, t.namespace], color: p.yellow },
        { tag: t.self, color: p.magenta },
        { tag: [t.typeName], color: p.yellow },
        { tag: [t.atom, t.bool, t.special(t.variableName)], color: p.yellow },
        { tag: [t.operator, t.operatorKeyword], color: p.red },
        { tag: t.keyword, color: p.green },
        { tag: t.controlKeyword, color: p.red },
        { tag: t.moduleKeyword, color: p.red },
        { tag: t.definitionKeyword, color: p.blue },
        { tag: [t.url, t.regexp, t.link], color: p.blue },
        { tag: [t.meta], color: p.tx2 },
        { tag: [t.punctuation, t.paren, t.bracket, t.squareBracket, t.brace, t.angleBracket], color: p.tx2 },
        { tag: t.attributeName, color: p.yellow },
        { tag: t.tagName, color: p.blue },
        { tag: t.invalid, color: p.red },
        { tag: t.strong, fontWeight: "bold" },
        { tag: t.emphasis, fontStyle: "italic" },
        { tag: t.link, color: p.blue, textDecoration: "underline" },
        { tag: t.heading, fontWeight: "bold", color: p.magenta },
        { tag: t.strikethrough, textDecoration: "line-through" },
    ])

    return [theme, syntaxHighlighting(highlight)]
}

export const flexokiDark: Extension = makeTheme(dark)
export const flexokiLight: Extension = makeTheme(light)
