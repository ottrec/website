#!/usr/bin/env python3
"""Generator for social-card.png, the og:image / twitter:image share card.

Renders the wordmark (Source Serif 4 bold) and tagline (Source Sans 3) from the
real webfont outlines in ./fonts, so the card matches the site without relying
on system fonts being installed. The card is 1200x630 (the standard
summary_large_image size).

To change the wordmark or tagline: edit WORDMARK / TAGLINE below, then re-run:

    pip install fonttools brotli           # one-time, for the woff2 outlines
    python3 social-card.py                  # writes social-card.svg
    magick -background none -density 144 social-card.svg -resize 1200x630 social-card.png

social-card.png is the served asset (registered in static.go); social-card.svg
is the intermediate source. Requires ImageMagick 7 (magick) for the PNG export.
"""
import copy, os
from fontTools.ttLib import TTFont
from fontTools.varLib.instancer import instantiateVariableFont
from fontTools.pens.svgPathPen import SVGPathPen

# ---- editable copy ------------------------------------------------------
WORDMARK = "ottrec.ca"          # a ".ca"/".com"/".org" tail is auto-muted
TAGLINE  = "Ottawa recreation schedules, in one place."
# -------------------------------------------------------------------------

HERE  = os.path.dirname(os.path.abspath(__file__))
FONTS = os.path.join(HERE, "fonts")
PAPER="#FFFCF0"; BASE100="#E6E4D9"; INK="#100F0F"; TX2="#6F6E69"
BLUE="#205EA6"; CYAN="#24837B"

def load(fn, **axes):
    f = TTFont(os.path.join(FONTS, fn + ".woff2"))
    inst = instantiateVariableFont(copy.deepcopy(f), axes, inplace=False)
    return inst.getGlyphSet(), inst.getBestCmap()
serif_gs, serif_cmap = load("source_serif_4", wght=700, opsz=60)
sans_gs,  sans_cmap  = load("source_sans_3",  wght=400)

def run(gs, cmap, text, s, x, baseline, fill):
    out=[]; pen=x
    for ch in text:
        g=gs[cmap[ord(ch)]]; p=SVGPathPen(gs); g.draw(p)
        out.append(f'<g transform="translate({pen:.2f},{baseline:.2f}) scale({s},{-s})"><path d="{p.getCommands()}" fill="{fill}"/></g>')
        pen += g.width*s
    return "".join(out), pen
def width(gs, cmap, text, s): return sum(gs[cmap[ord(c)]].width*s for c in text)
def wave(x0, x1, y, a, w, periods=1.5):
    half=int(round(periods*2)); seg=(x1-x0)/half; d=f"M{x0:.2f} {y:.2f}"; x=x0; up=True
    for i in range(half):
        cy = y-a if up else y+a
        d += f" C{x+seg*0.42:.2f} {cy:.2f} {x+seg*0.58:.2f} {cy:.2f} {x+seg:.2f} {y:.2f}"; x+=seg; up=not up
    return f'<path d="{d}" fill="none" stroke="url(#wave)" stroke-width="{w}" stroke-linecap="round"/>'

def build(wordmark, tagline):
    W, H = 1200, 630
    defs=(f'<defs><linearGradient id="bg" x1="0" y1="0" x2="0.4" y2="1"><stop offset="0" stop-color="{PAPER}"/><stop offset="1" stop-color="{BASE100}"/></linearGradient>'
          f'<linearGradient id="wave" x1="0" y1="0" x2="1" y2="0"><stop offset="0" stop-color="{BLUE}"/><stop offset="1" stop-color="{CYAN}"/></linearGradient></defs>')
    b = f'<rect width="{W}" height="{H}" fill="url(#bg)"/>'
    # wordmark, centred; mute a trailing TLD tail (".ca") in tx-2
    s, base = 0.22, 300
    tail = next((t for t in (".ca", ".com", ".org") if wordmark.endswith(t)), "")
    head = wordmark[:len(wordmark)-len(tail)] if tail else wordmark
    x0 = (W - width(serif_gs, serif_cmap, wordmark, s)) / 2
    hp, xmid = run(serif_gs, serif_cmap, head, s, x0, base, INK)
    tp, xend = run(serif_gs, serif_cmap, tail, s, xmid, base, TX2) if tail else ("", xmid)
    b += hp + tp
    # double wave underline spanning the wordmark
    wy = base + 58
    b += wave(x0, xend, wy, 11, 11) + wave(x0-12, xend+12, wy+24, 12, 11)
    # tagline (sans, muted), centred
    ts = 0.046
    tg, _ = run(sans_gs, sans_cmap, tagline, ts, (W - width(sans_gs, sans_cmap, tagline, ts))/2, base+150, TX2)
    b += tg
    return f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {W} {H}">\n{defs}{b}\n</svg>\n'

if __name__ == "__main__":
    open(os.path.join(HERE, "social-card.svg"), "w").write(build(WORDMARK, TAGLINE))
    print("wrote social-card.svg — now run the magick command in this file's docstring")
