import './component'
import type { OttrecqlEditor } from './component'

function siteTheme(): 'tokyo-night' | 'tokyo-night-day' {
    const cs = document.documentElement.style.colorScheme
    const dark = cs === 'dark' || (cs !== 'light' && matchMedia('(prefers-color-scheme: dark)').matches)
    return dark ? 'tokyo-night' : 'tokyo-night-day'
}

const editors: OttrecqlEditor[] = []

for (const ta of document.querySelectorAll<HTMLTextAreaElement>('textarea[data-ottrecql]')) {
    const ed = document.createElement('ottrecql-editor')
    if (ta.name) ed.setAttribute('name', ta.name)
    ed.setAttribute('value', ta.value)
    if (ta.required) ed.setAttribute('required', '')
    if (ta.placeholder) ed.setAttribute('placeholder', ta.placeholder)
    const label = ta.getAttribute('aria-label')
    if (label) ed.setAttribute('aria-label', label)
    ed.setAttribute('theme', siteTheme())
    ta.replaceWith(ed)
    ed.lint = ta.dataset['lint'] ?? null // ottrec.ca query-validation endpoint
    editors.push(ed)
}

if (editors.length) {
    const applyTheme = () => {
        const theme = siteTheme()
        for (const ed of editors) ed.theme = theme
    }
    window.addEventListener('themechange', applyTheme) // the navbar toggle (theme.ts)
    matchMedia('(prefers-color-scheme: dark)').addEventListener('change', applyTheme)
}
