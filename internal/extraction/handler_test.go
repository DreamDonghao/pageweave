package extraction

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DreamDonghao/pageweave/internal/config"
	"github.com/DreamDonghao/pageweave/internal/network"
)

type fakeRenderer struct {
	render func(context.Context, Request) (Snapshot, error)
}

func (f fakeRenderer) Ready() bool { return true }
func (f fakeRenderer) Render(ctx context.Context, r Request) (Snapshot, error) {
	if f.render != nil {
		return f.render(ctx, r)
	}
	return Snapshot{HTML: "<html><title>测试</title><body><main><p>有效短正文</p></main></body></html>", URL: r.URL, Title: "测试"}, nil
}
func testConfig(t *testing.T) config.Config {
	t.Helper()
	c, e := config.Load()
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
func TestHandlerContract(t *testing.T) {
	c := testConfig(t)
	c.MaxRequestBytes = 300
	s := NewService(c, fakeRenderer{}, network.Policy{FixtureURL: "https://fixture.test"}, quietLog())
	defer s.Shutdown(context.Background())
	h := Handler{s, c, quietLog()}
	tests := []struct {
		name, body, media string
		status            int
		code              string
	}{
		{"defaults", `{"url":"https://fixture.test/x"}`, "application/json", 200, ""},
		{"false", `{"url":"https://fixture.test","include_links":false,"include_images":false}`, "application/json", 200, ""},
		{"zero", `{"url":"https://fixture.test","max_chars":0}`, "application/json", 422, "invalid_request"},
		{"null", `{"url":"https://fixture.test","include_links":null}`, "application/json", 422, "invalid_request"},
		{"selector null", `{"url":"https://fixture.test","wait_for_selector":null}`, "application/json", 200, ""},
		{"unknown", `{"url":"https://fixture.test","unexpected":1}`, "application/json", 400, "invalid_json"},
		{"multiple", `{"url":"https://fixture.test"}{}`, "application/json", 400, "invalid_json"},
		{"trailing", `{"url":"https://fixture.test"}x`, "application/json", 400, "invalid_json"},
		{"syntax", `{`, "application/json", 400, "invalid_json"},
		{"object", `null`, "application/json", 400, "invalid_json"},
		{"type", `{"url":3}`, "application/json", 400, "invalid_json"},
		{"missing", `{}`, "application/json", 422, "invalid_request"},
		{"format", `{"url":"https://fixture.test","format":"html"}`, "application/json", 422, "invalid_request"},
		{"credentials", `{"url":"https://u:p@fixture.test"}`, "application/json", 422, "invalid_request"},
		{"blocked", `{"url":"http://127.0.0.1"}`, "application/json", 403, "blocked_url"},
		{"media", `{}`, "text/plain", 415, "unsupported_media_type"},
		{"large", strings.Repeat("x", 301), "application/json", 413, "content_too_large"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/extract", strings.NewReader(tt.body))
			r.Header.Set("Content-Type", tt.media)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tt.status {
				t.Fatalf("%d: %s", w.Code, w.Body)
			}
			var result struct {
				RequestID string `json:"request_id"`
				Error     *Error `json:"error"`
				Response
			}
			if e := json.Unmarshal(w.Body.Bytes(), &result); e != nil {
				t.Fatal(e)
			}
			if w.Header().Get("X-Request-ID") == "" {
				t.Fatal("missing ID")
			}
			if tt.code != "" && (result.Error == nil || result.Error.Code != tt.code) {
				t.Fatalf("error %s", w.Body)
			}
			if tt.name == "defaults" && (result.Format != "markdown" || result.ContentScope != "full") {
				t.Fatal(w.Body)
			}
		})
	}
	r := httptest.NewRequest("GET", "/extract", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 405 || w.Header().Get("Allow") != "POST" {
		t.Fatal(w.Code)
	}
}
func TestHandlerPreservesExplicitFields(t *testing.T) {
	c := testConfig(t)
	var got Request
	f := fakeRenderer{render: func(ctx context.Context, r Request) (Snapshot, error) {
		got = r
		return Snapshot{HTML: `<main><p>甲乙</p></main>`, URL: r.URL}, nil
	}}
	s := NewService(c, f, network.Policy{FixtureURL: "https://fixture.test"}, quietLog())
	defer s.Shutdown(context.Background())
	h := Handler{s, c, quietLog()}
	req := httptest.NewRequest("POST", "/extract", strings.NewReader(`{"url":"https://fixture.test","format":"text","content_scope":"full","include_links":false,"include_images":true,"max_chars":1,"scroll":true}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 || got.IncludeLinks || !got.IncludeImages || !got.Scroll || got.Format != "text" || got.ContentScope != "full" || got.MaxChars != 1 {
		t.Fatal(got, w.Body)
	}
	var response Response
	if e := json.Unmarshal(w.Body.Bytes(), &response); e != nil {
		t.Fatal(e)
	}
	if response.Content != "甲" || !response.Truncated {
		t.Fatal(response)
	}
}

func TestDefaultFullKeepsAdditionalRegions(t *testing.T) {
	c := testConfig(t)
	renderer := fakeRenderer{render: func(ctx context.Context, r Request) (Snapshot, error) {
		return Snapshot{HTML: richHTML, URL: r.URL, Title: "首页"}, nil
	}}
	service := NewService(c, renderer, network.Policy{FixtureURL: "https://fixture.test"}, quietLog())
	defer service.Shutdown(context.Background())
	handler := Handler{Service: service, Config: c, Log: quietLog()}
	for _, tc := range []struct {
		name, body, scope string
		additional        bool
	}{
		{"omitted", `{"url":"https://fixture.test"}`, "full", true},
		{"explicit main", `{"url":"https://fixture.test","content_scope":"main"}`, "main", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/extract", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Code != 200 {
				t.Fatal(recorder.Code, recorder.Body)
			}
			var response Response
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.ContentScope != tc.scope || strings.Contains(response.Content, "整页附加内容") != tc.additional {
				t.Fatal(response)
			}
		})
	}
}
