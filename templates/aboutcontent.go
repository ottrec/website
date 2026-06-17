package templates

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/ottrec/website/internal/asset"
	"github.com/ottrec/website/pkg/ottrecidx"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// aboutContentFS holds the markdown sources for the generic /about/{slug} pages,
// one file per page named about-{slug}.md. They're rendered to HTML once at
// startup (see aboutContentPages); the set of pages actually served is the
// hard-coded list in the routes package, not this directory.
//
//go:embed about-*.md
var aboutContentFS embed.FS

// aboutMarkdown renders the about pages: GitHub-flavored tables, autolinks,
// strikethrough, and smart typography (curly quotes, em/en dashes, ellipses) on
// top of CommonMark. Headings get auto-generated ids and a hover anchor link
// (see aboutHeadingRenderer) so they can be linked to.
var aboutMarkdown = goldmark.New(
	goldmark.WithExtensions(extension.Table, extension.Linkify, extension.Strikethrough, extension.Typographer),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	goldmark.WithRendererOptions(renderer.WithNodeRenderers(util.Prioritized(aboutHeadingRenderer{}, 100))),
)

// aboutHeadingRenderer overrides goldmark's default heading rendering to append
// a self-link to each heading that has an id (revealed on hover via CSS). It
// otherwise matches the default renderer.
type aboutHeadingRenderer struct{}

func (aboutHeadingRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindHeading, renderAboutHeading)
}

func renderAboutHeading(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Heading)
	if entering {
		_, _ = w.WriteString("<h")
		_ = w.WriteByte("0123456"[n.Level])
		if n.Attributes() != nil {
			html.RenderAttributes(w, node, html.HeadingAttributeFilter)
		}
		_ = w.WriteByte('>')
	} else {
		if id, ok := n.AttributeString("id"); ok {
			if idb, ok := id.([]byte); ok {
				_, _ = w.WriteString(`<a class="heading-anchor" href="#`)
				_, _ = w.Write(util.EscapeHTML(util.URLEscape(idb, false)))
				_, _ = w.WriteString(`" aria-label="Permalink to this section">#</a>`)
			}
		}
		_, _ = w.WriteString("</h")
		_ = w.WriteByte("0123456"[n.Level])
		_, _ = w.WriteString(">\n")
	}
	return ast.WalkContinue, nil
}

// aboutContentBlock is a dynamic, data-driven region of an about page, embedded
// in the markdown with a "```block <name>```" fence (see aboutContentBlocks).
// The markdown supplies the prose around it; the block supplies the interactive
// or data-derived parts, like the regions map and centrepoint table.
type aboutContentBlock struct {
	// Body renders in place of the block fence, between the surrounding prose.
	Body func(ottrecidx.DataRef) templ.Component
	// CSS is added to the page <head> when the block is used.
	CSS []*asset.Asset
	// Foot, if set, renders at the end of <body> (e.g. a JSON data island and
	// the script that reads it).
	Foot func(ottrecidx.DataRef) templ.Component
}

// aboutContentPart is one piece of a rendered about page, in document order:
// either a chunk of static HTML rendered from the markdown, or a dynamic block.
type aboutContentPart struct {
	html  templ.Component // set for a static markdown chunk
	block string          // set for a dynamic block (a key into aboutContentBlocks)
}

// AboutContent is a single rendered /about/{slug} markdown page.
type AboutContent struct {
	Slug string
	// Title is the frontmatter title, else the first level-1 heading; used for
	// <title>/social and rendered as the page <h1>. Description is the long
	// meta/social description: the frontmatter description, else the first
	// paragraph.
	Title       string
	Description string

	// Date and Author come from frontmatter and, when set, render as a byline
	// under the title. DateISO is Date parsed to "2006-01-02" (empty if it
	// couldn't be parsed), for <time datetime> and article:published_time.
	Date    string
	DateISO string
	Author  string

	// The /about and homepage listings show short link text and a one-line
	// blurb, and they deliberately differ from each other and from the long
	// Description, so each is its own frontmatter field.
	AboutLabel string // /about articles list link text (defaults to the lowercased title)
	AboutBlurb string // /about articles list one-line description
	HomeLabel  string // homepage "More stuff" list link text
	HomeBlurb  string // homepage "More stuff" list one-line description

	BodyClass string // the <body> class (frontmatter class appends to about-content-page)

	parts  []aboutContentPart // the body, in document order
	blocks []string           // distinct dynamic blocks used, in first-seen order
}

// aboutContentPages is every about-*.md file rendered to HTML, keyed by slug. It
// is built at startup and panics if any file fails to render or lacks a title,
// so content mistakes surface immediately rather than on first request.
var aboutContentPages = func() map[string]*AboutContent {
	entries, err := fs.Glob(aboutContentFS, "about-*.md")
	if err != nil {
		panic(fmt.Errorf("about content: glob: %w", err))
	}
	pages := make(map[string]*AboutContent, len(entries))
	for _, name := range entries {
		slug := strings.TrimSuffix(strings.TrimPrefix(name, "about-"), ".md")
		src, err := aboutContentFS.ReadFile(name)
		if err != nil {
			panic(fmt.Errorf("about content %q: %w", name, err))
		}
		page, err := renderAboutContent(slug, src)
		if err != nil {
			panic(fmt.Errorf("about content %q: %w", name, err))
		}
		pages[slug] = page
	}
	return pages
}()

// renderAboutContent renders a single about page's markdown to HTML and pulls
// out its metadata. The body is split into static HTML chunks and dynamic
// blocks (see aboutContentPart) so the data-driven parts can be rendered per
// request, around the prose, by WebsiteAboutContentPage.
func renderAboutContent(slug string, src []byte) (*AboutContent, error) {
	meta, body, err := parseFrontmatter(src)
	if err != nil {
		return nil, err
	}

	doc := aboutMarkdown.Parser().Parse(text.NewReader(body))

	var title, desc string
	for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
		switch n := n.(type) {
		case *ast.Heading:
			if title == "" && n.Level == 1 {
				title = nodeText(n, body)
			}
		case *ast.Paragraph:
			if desc == "" {
				desc = strings.Join(strings.Fields(nodeText(n, body)), " ")
			}
		}
	}
	if v := meta["title"]; v != "" {
		title = v
	}
	if v := meta["description"]; v != "" {
		desc = v
	}
	if title == "" {
		return nil, fmt.Errorf("missing a level-1 heading or frontmatter title")
	}

	content := &AboutContent{
		Slug:        slug,
		Title:       title,
		Description: desc,
		Date:        meta["date"],
		DateISO:     aboutDateISO(meta["date"]),
		Author:      meta["author"],
		AboutLabel:  meta["about_label"],
		AboutBlurb:  meta["about_blurb"],
		HomeLabel:   meta["home_label"],
		HomeBlurb:   meta["home_blurb"],
		BodyClass:   "about-content-page",
	}
	if content.AboutLabel == "" {
		content.AboutLabel = strings.ToLower(title)
	}
	if v := meta["class"]; v != "" {
		content.BodyClass += " " + v
	}

	var buf bytes.Buffer
	seen := map[string]bool{}
	flush := func() {
		if buf.Len() > 0 {
			content.parts = append(content.parts, aboutContentPart{html: templ.Raw(buf.String())})
			buf.Reset()
		}
	}
	for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
		if name, ok := blockFenceName(n, body); ok {
			if _, exists := aboutContentBlocks[name]; !exists {
				return nil, fmt.Errorf("unknown block %q", name)
			}
			flush()
			content.parts = append(content.parts, aboutContentPart{block: name})
			if !seen[name] {
				seen[name] = true
				content.blocks = append(content.blocks, name)
			}
			continue
		}
		if err := aboutMarkdown.Renderer().Render(&buf, body, n); err != nil {
			return nil, fmt.Errorf("render: %w", err)
		}
	}
	flush()

	return content, nil
}

// parseFrontmatter splits an optional leading frontmatter block, delimited by
// lines of "---", from the markdown body. Only simple "key: value" pairs are
// supported: values are trimmed and any surrounding quotes are stripped.
func parseFrontmatter(src []byte) (map[string]string, []byte, error) {
	meta := map[string]string{}
	if !bytes.HasPrefix(src, []byte("---\n")) && !bytes.HasPrefix(src, []byte("---\r\n")) {
		return meta, src, nil
	}
	lines := strings.Split(string(src), "\n")
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r") == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return nil, nil, fmt.Errorf("unterminated frontmatter")
	}
	for i := 1; i < end; i++ {
		line := strings.TrimRight(lines[i], "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			return nil, nil, fmt.Errorf("invalid frontmatter line %q", line)
		}
		meta[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return meta, []byte(strings.Join(lines[end+1:], "\n")), nil
}

// aboutDateISO parses a frontmatter date into "2006-01-02", returning "" if it
// doesn't match one of the accepted layouts.
func aboutDateISO(s string) string {
	if s == "" {
		return ""
	}
	for _, layout := range []string{"January 2, 2006", "Jan 2, 2006", "2 January 2006", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return ""
}

// blockFenceName reports whether n is a dynamic block fence ("```block <name>```")
// and returns the block name.
func blockFenceName(n ast.Node, src []byte) (string, bool) {
	fcb, ok := n.(*ast.FencedCodeBlock)
	if !ok || fcb.Info == nil {
		return "", false
	}
	if fields := strings.Fields(string(fcb.Info.Segment.Value(src))); len(fields) == 2 && fields[0] == "block" {
		return fields[1], true
	}
	return "", false
}

// HeadCSS returns the stylesheets the page's blocks need in <head>, deduplicated
// in first-use order.
func (c *AboutContent) HeadCSS() []*asset.Asset {
	var out []*asset.Asset
	seen := map[*asset.Asset]bool{}
	for _, name := range c.blocks {
		for _, a := range aboutContentBlocks[name].CSS {
			if !seen[a] {
				seen[a] = true
				out = append(out, a)
			}
		}
	}
	return out
}

// aboutBlockBody renders the body of a named block.
func aboutBlockBody(name string, data ottrecidx.DataRef) templ.Component {
	return aboutContentBlocks[name].Body(data)
}

// aboutBlockFoot renders the end-of-body content for a named block, if any.
func aboutBlockFoot(name string, data ottrecidx.DataRef) templ.Component {
	if f := aboutContentBlocks[name].Foot; f != nil {
		return f(data)
	}
	return templ.NopComponent
}

// nodeText returns the concatenated text of a node's inline descendants.
func nodeText(n ast.Node, src []byte) string {
	var b strings.Builder
	ast.Walk(n, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if t, ok := node.(*ast.Text); ok {
				b.Write(t.Segment.Value(src))
			}
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}

// AboutContentBySlug returns the rendered about page for slug, if one exists.
func AboutContentBySlug(slug string) (*AboutContent, bool) {
	p, ok := aboutContentPages[slug]
	return p, ok
}

// aboutArticleSlugs returns the markdown-backed about page slugs in a stable
// order, for the Articles section on the /about page.
func aboutArticleSlugs() []string {
	slugs := make([]string, 0, len(aboutContentPages))
	for s := range aboutContentPages {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	return slugs
}
