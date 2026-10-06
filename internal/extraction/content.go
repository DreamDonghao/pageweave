package extraction

import (
	"bytes"
	"context"
	"net/url"
	"strings"
	"unicode"

	readability "codeberg.org/readeck/go-readability/v2"
	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"golang.org/x/net/html"
)

// Convert cleans a bounded rendered snapshot and converts it synchronously.
// It checks cancellation between parsing stages and limits output by Unicode code point.
func Convert(ctx context.Context, s Snapshot, r Request) (Response, error) {
	out := Response{URL: r.URL, FinalURL: s.URL, Title: s.Title, Format: r.Format, ContentScope: r.ContentScope, Warnings: append([]string{}, s.Warnings...)}
	if e := ctx.Err(); e != nil {
		return out, e
	}
	doc, e := html.Parse(strings.NewReader(s.HTML))
	if e != nil {
		return out, e
	}
	final, e := url.Parse(s.URL)
	if e != nil {
		return out, e
	}
	baseURL := final
	if n := find(doc, "base"); n != nil {
		if u, e := url.Parse(attr(n, "href")); e == nil {
			resolved := final.ResolveReference(u)
			if resolved.Scheme == "http" || resolved.Scheme == "https" {
				baseURL = resolved
			}
		}
	}
	clean(doc)
	body := find(doc, "body")
	if body == nil {
		body = doc
	}
	selected := body
	if r.ContentScope == "main" {
		var buf bytes.Buffer
		if e = html.Render(&buf, doc); e != nil {
			return out, e
		}
		article, readErr := readability.FromReader(&buf, final)
		if readErr == nil && article.Node != nil && strings.TrimSpace(plainText(article.Node)) != "" {
			selected = article.Node
			if title := article.Title(); title != "" {
				out.Title = title
			}
		} else {
			if main := find(body, "main", "article"); main != nil && strings.TrimSpace(plainText(main)) != "" {
				selected = main
			}
			out.Warnings = append(out.Warnings, "fallback_full_content")
		}
	}
	resolve(selected, baseURL, r)
	if e = ctx.Err(); e != nil {
		return out, e
	}
	if !meaningful(plainText(selected)) {
		return out, problem(422, "no_content", "网页没有有效正文", nil)
	}
	if r.Format == "text" {
		out.Content = plainText(selected)
	} else {
		conv := converter.NewConverter(converter.WithPlugins(base.NewBasePlugin(), commonmark.NewCommonmarkPlugin(), table.NewTablePlugin(table.WithNewlineBehavior(table.NewlineBehaviorPreserve))))
		result, e := conv.ConvertNode(selected)
		if e != nil {
			return out, e
		}
		out.Content = strings.TrimSpace(string(result))
	}
	if e = ctx.Err(); e != nil {
		return out, e
	}
	if out.Content == "" {
		return out, problem(422, "no_content", "网页没有有效正文", nil)
	}
	runes := []rune(out.Content)
	if len(runes) > r.MaxChars {
		out.Content = string(runes[:r.MaxChars])
		out.Truncated = true
	}
	return out, nil
}
func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
func setAttr(n *html.Node, key, value string) {
	for i := range n.Attr {
		if n.Attr[i].Key == key {
			n.Attr[i].Val = value
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: key, Val: value})
}
func delAttr(n *html.Node, key string) {
	a := n.Attr[:0]
	for _, v := range n.Attr {
		if v.Key != key {
			a = append(a, v)
		}
	}
	n.Attr = a
}
func find(n *html.Node, tags ...string) *html.Node {
	if n.Type == html.ElementNode {
		for _, tag := range tags {
			if n.Data == tag {
				return n
			}
		}
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if found := find(child, tags...); found != nil {
			return found
		}
	}
	return nil
}
func clean(n *html.Node) {
	for child := n.FirstChild; child != nil; {
		next := child.NextSibling
		remove := child.Type == html.CommentNode
		if child.Type == html.ElementNode {
			switch child.Data {
			case "script", "style", "noscript", "template", "nav", "iframe", "object", "embed", "form":
				remove = true
			}
			if child.Data == "footer" {
				withinContent := false
				for parent := child.Parent; parent != nil; parent = parent.Parent {
					if parent.Data == "article" || parent.Data == "main" {
						withinContent = true
						break
					}
				}
				if !withinContent {
					remove = true
				}
			}
			if attr(child, "aria-hidden") == "true" || attr(child, "role") == "navigation" || attr(child, "role") == "contentinfo" {
				remove = true
			}
			for _, a := range child.Attr {
				if a.Key == "hidden" {
					remove = true
				}
			}
		}
		if remove {
			n.RemoveChild(child)
		} else {
			clean(child)
		}
		child = next
	}
}
func resolve(n *html.Node, baseURL *url.URL, r Request) {
	if n.Type == html.ElementNode {
		for _, key := range []string{"href", "src"} {
			if value := attr(n, key); value != "" {
				u, e := url.Parse(value)
				if e != nil {
					delAttr(n, key)
				} else {
					u = baseURL.ResolveReference(u)
					if u.Scheme == "http" || u.Scheme == "https" || (key == "href" && u.Scheme == "mailto") {
						setAttr(n, key, u.String())
					} else {
						delAttr(n, key)
					}
				}
			}
		}
		if n.Data == "a" && !r.IncludeLinks {
			n.Data = "span"
			delAttr(n, "href")
		}
		if n.Data == "img" && (!r.IncludeImages || attr(n, "src") == "") {
			n.Type = html.TextNode
			n.Data = attr(n, "alt")
			n.Attr = nil
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		resolve(c, baseURL, r)
	}
}
func plainText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node, bool)
	walk = func(n *html.Node, pre bool) {
		if n.Type == html.TextNode {
			if pre {
				b.WriteString(n.Data)
			} else {
				text := strings.Join(strings.Fields(n.Data), " ")
				runes := []rune(n.Data)
				if len(runes) > 0 && unicode.IsSpace(runes[0]) {
					b.WriteByte(' ')
				}
				b.WriteString(text)
				if len(runes) > 0 && text != "" && unicode.IsSpace(runes[len(runes)-1]) {
					b.WriteByte(' ')
				}
			}
			return
		}
		block := false
		switch n.Data {
		case "p", "div", "article", "main", "section", "h1", "h2", "h3", "h4", "h5", "h6", "li", "blockquote", "pre", "tr", "table":
			block = true
		}
		if block {
			b.WriteByte('\n')
		}
		if n.Data == "br" {
			b.WriteByte('\n')
		}
		if n.Data == "img" {
			b.WriteString(attr(n, "alt"))
			if attr(n, "src") != "" {
				b.WriteString(" (" + attr(n, "src") + ")")
			}
		}
		if n.Data == "td" || n.Data == "th" {
			b.WriteByte('\t')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, pre || n.Data == "pre")
		}
		if block {
			b.WriteByte('\n')
		}
	}
	walk(n, false)
	return strings.TrimSpace(b.String())
}

var loadingPlaceholders = []string{"loading", "加载中", "正在加载", "please wait", "请稍候", "请稍等"}

// Pages consisting only of these auxiliary labels are still loading shells.
// This affects readiness/empty-content checks, not removal of paragraphs.
var auxiliaryLabels = []string{"skip to content", "skip to main content", "accessibility feedback"}

func meaningful(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	normalized := strings.TrimRight(strings.ToLower(text), ".。…!！ \t\r\n")
	for _, placeholder := range loadingPlaceholders {
		if normalized == placeholder {
			return false
		}
	}
	canonical := strings.Join(strings.Fields(normalized), " ")
	for _, label := range auxiliaryLabels {
		canonical = strings.ReplaceAll(canonical, label, "")
	}
	return strings.TrimSpace(canonical) != ""
}
