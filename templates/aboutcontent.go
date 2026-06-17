package templates

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/a-h/templ"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

// aboutContentFS holds the markdown sources for the generic /about/{slug} pages,
// one file per page named about-{slug}.md. They're rendered to HTML once at
// startup (see aboutContentPages); the set of pages actually served is the
// hard-coded list in the routes package, not this directory.
//
//go:embed about-*.md
var aboutContentFS embed.FS

// aboutMarkdown renders the about pages: GitHub-flavored tables, autolinks, and
// strikethrough on top of CommonMark.
var aboutMarkdown = goldmark.New(
	goldmark.WithExtensions(extension.Table, extension.Linkify, extension.Strikethrough),
)

// AboutContent is a single rendered /about/{slug} markdown page.
type AboutContent struct {
	Slug        string
	Title       string          // the first level-1 heading, for <title>
	Description string          // the first paragraph, for <meta name="description">
	Body        templ.Component // the rendered HTML, placed under .about-content
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
// out its title (first level-1 heading) and description (first paragraph).
func renderAboutContent(slug string, src []byte) (*AboutContent, error) {
	doc := aboutMarkdown.Parser().Parse(text.NewReader(src))

	var title, desc string
	for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
		switch n := n.(type) {
		case *ast.Heading:
			if title == "" && n.Level == 1 {
				title = nodeText(n, src)
			}
		case *ast.Paragraph:
			if desc == "" {
				desc = strings.Join(strings.Fields(nodeText(n, src)), " ")
			}
		}
	}
	if title == "" {
		return nil, fmt.Errorf("missing a level-1 heading for the title")
	}

	var buf bytes.Buffer
	if err := aboutMarkdown.Renderer().Render(&buf, src, doc); err != nil {
		return nil, fmt.Errorf("render: %w", err)
	}
	return &AboutContent{
		Slug:        slug,
		Title:       title,
		Description: desc,
		Body:        templ.Raw(buf.String()),
	}, nil
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

// aboutArticleDescription returns the short blurb shown for a markdown-backed
// about page in the Articles section, keyed by slug.
func aboutArticleDescription(slug string) string {
	switch slug {
	case "data-quality":
		return "How the schedule data is scraped, parsed, and verified (for the technically inclined)."
	default:
		return ""
	}
}
