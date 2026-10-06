package extraction

import (
	"context"
	_ "embed"
	"strings"
	"testing"
	"unicode/utf8"
)

//go:embed testdata/rich.html
var richHTML string

func TestContentFormats(t *testing.T) {
	for _, format := range []string{"markdown", "text"} {
		t.Run(format, func(t *testing.T) {
			r := DefaultRequest()
			r.Format = format
			r.ContentScope = "full"
			r.IncludeImages = true
			out, e := Convert(context.Background(), Snapshot{HTML: richHTML, URL: "https://example.org/start", Title: "示例"}, r)
			if e != nil {
				t.Fatal(e)
			}
			for _, want := range []string{"中文标题", "短句", "链接", "图片说明", "引用", "列表", "fmt.Println", "甲", "丙", "整页附加内容"} {
				if !strings.Contains(out.Content, want) {
					t.Errorf("missing %q: %s", want, out.Content)
				}
			}
			for _, noise := range []string{"导航噪声", "页脚噪声", "隐藏噪声"} {
				if strings.Contains(out.Content, noise) {
					t.Error(noise)
				}
			}
			if format == "markdown" {
				for _, want := range []string{"https://cdn.example.org/docs/next", "https://cdn.example.org/docs/img.png", "```go", "|"} {
					if !strings.Contains(out.Content, want) {
						t.Errorf("missing %q: %s", want, out.Content)
					}
				}
			} else if strings.Contains(out.Content, "](https:") {
				t.Fatal(out.Content)
			}
		})
	}
}
func TestContentOptions(t *testing.T) {
	r := DefaultRequest()
	r.ContentScope = "full"
	r.IncludeLinks = false
	out, e := Convert(context.Background(), Snapshot{HTML: richHTML, URL: "https://example.org"}, r)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(out.Content, "https://") || !strings.Contains(out.Content, "链接") || !strings.Contains(out.Content, "图片说明") {
		t.Fatal(out.Content)
	}
	r.MaxChars = 3
	out, e = Convert(context.Background(), Snapshot{HTML: "<p>甲乙丙丁😀</p>", URL: "https://example.org"}, r)
	if e != nil {
		t.Fatal(e)
	}
	if !out.Truncated || utf8.RuneCountInString(out.Content) != 3 || !utf8.ValidString(out.Content) {
		t.Fatal(out)
	}
}
func TestMainAndFallback(t *testing.T) {
	r := DefaultRequest()
	r.ContentScope = "main"
	r.Format = "text"
	out, e := Convert(context.Background(), Snapshot{HTML: richHTML, URL: "https://example.org"}, r)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(out.Content, "整页附加内容") {
		t.Fatal(out.Content)
	}
	out, e = Convert(context.Background(), Snapshot{HTML: "<main><p>合法短正文</p></main>", URL: "https://example.org"}, r)
	if e != nil || !strings.Contains(out.Content, "合法短正文") {
		t.Fatal(out, e)
	}
	_, e = Convert(context.Background(), Snapshot{HTML: "<script>x</script>", URL: "https://example.org"}, r)
	if e == nil {
		t.Fatal("empty succeeded")
	}
}
func TestFilterBoundaries(t *testing.T) {
	r := DefaultRequest()
	r.ContentScope = "full"
	r.Format = "text"
	out, e := Convert(context.Background(), Snapshot{HTML: `<article><footer>文章来源</footer></article><p class="footer-note">合法内容</p><p>重复词 重复词</p><div><p>短</p></div><nav>菜单</nav>`, URL: "https://example.org"}, r)
	if e != nil {
		t.Fatal(e)
	}
	for _, want := range []string{"文章来源", "合法内容", "重复词 重复词", "短"} {
		if !strings.Contains(out.Content, want) {
			t.Fatal(out.Content)
		}
	}
	if strings.Contains(out.Content, "菜单") {
		t.Fatal(out.Content)
	}
}

func TestTextWhitespaceAndUnsafeURLs(t *testing.T) {
	r := DefaultRequest()
	r.Format = "text"
	r.ContentScope = "full"
	out, e := Convert(context.Background(), Snapshot{HTML: `<p>甲 <a href="javascript:alert(1)">链接</a> 乙</p>`, URL: "https://example.org"}, r)
	if e != nil || out.Content != "甲 链接 乙" {
		t.Fatal(out, e)
	}
	r.Format = "markdown"
	out, e = Convert(context.Background(), Snapshot{HTML: `<p><a href="javascript:alert(1)">保留文字</a></p><img src="data:image/png,x" alt="替代文字">`, URL: "https://example.org"}, r)
	if e != nil || strings.Contains(out.Content, "javascript:") || strings.Contains(out.Content, "data:") {
		t.Fatal(out, e)
	}
}

func TestLoadingPlaceholder(t *testing.T) {
	r := DefaultRequest()
	r.ContentScope = "full"
	for _, text := range []string{"Loading...", "加载中…", "请稍候"} {
		_, e := Convert(context.Background(), Snapshot{HTML: "<main>" + text + "</main>", URL: "https://example.org"}, r)
		if e == nil {
			t.Fatal("placeholder accepted:", text)
		}
	}
	if _, e := Convert(context.Background(), Snapshot{HTML: "<p>正在加载的历史</p>", URL: "https://example.org"}, r); e != nil {
		t.Fatal(e)
	}
}

func TestAuxiliaryOnlyPageIsNotContent(t *testing.T) {
	r := DefaultRequest()
	for _, text := range []string{"Skip to content\nAccessibility Feedback", "  SKIP TO MAIN CONTENT  \nAccessibility Feedback"} {
		_, e := Convert(context.Background(), Snapshot{HTML: "<p>" + text + "</p>", URL: "https://example.org"}, r)
		if e == nil {
			t.Fatal("auxiliary-only page accepted")
		}
	}
	for _, text := range []string{"Accessibility Feedback 的用途说明", "短", "Skip to content\n有效正文"} {
		out, e := Convert(context.Background(), Snapshot{HTML: "<p>" + text + "</p>", URL: "https://example.org"}, r)
		if e != nil || out.Content == "" {
			t.Fatal(out, e)
		}
	}
}
