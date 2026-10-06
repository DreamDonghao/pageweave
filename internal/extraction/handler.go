package extraction

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"time"

	"github.com/DreamDonghao/pageweave/internal/config"
	"github.com/DreamDonghao/pageweave/internal/network"
)

// Handler enforces the JSON contract and correlates responses with request logs.
type Handler struct {
	Service *Service
	Config  config.Config
	Log     *slog.Logger
}

// RequestID creates an opaque identifier without accepting caller-controlled log fields.
func RequestID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "req-" + time.Now().Format("20060102150405.000000000")
	}
	return "req-" + hex.EncodeToString(b[:])
}

// WriteError emits the shared JSON error envelope.
func WriteError(w http.ResponseWriter, id string, e *Error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Request-ID", id)
	if e.Status == 429 {
		w.Header().Set("Retry-After", "1")
	}
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(struct {
		RequestID string `json:"request_id"`
		Error     *Error `json:"error"`
	}{id, e})
}

// ServeHTTP handles one extraction request without exposing private error causes.
func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := RequestID()
	ctx, cancel := context.WithTimeout(r.Context(), h.Config.RequestTimeout)
	defer cancel()
	r = r.WithContext(ctx)
	w.Header().Set("X-Request-ID", id)
	fail := func(e *Error) {
		stage := map[string]string{"invalid_json": "request", "invalid_request": "validation", "blocked_url": "network", "upstream_error": "navigation", "no_content": "content", "extraction_timeout": "budget", "browser_unavailable": "browser", "too_many_requests": "admission"}[e.Code]
		h.Log.Warn("extract_failed", "request_id", id, "code", e.Code, "stage", stage, "reason", e.Message, "cause_type", fmt.Sprintf("%T", e.Cause))
		WriteError(w, id, e)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			h.Log.Error("unexpected_handler_panic", "request_id", id, "panic_type", fmt.Sprintf("%T", recovered))
			WriteError(w, id, problem(500, "internal_error", "网页提取内部错误", nil))
		}
	}()
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		fail(problem(405, "method_not_allowed", "只支持 POST", nil))
		return
	}
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || media != "application/json" {
		fail(problem(415, "unsupported_media_type", "Content-Type 必须为 application/json", nil))
		return
	}
	body := http.MaxBytesReader(w, r.Body, int64(h.Config.MaxRequestBytes))
	defer body.Close()
	data, e := io.ReadAll(body)
	if e != nil {
		var limit *http.MaxBytesError
		if errors.As(e, &limit) {
			fail(problem(413, "content_too_large", "请求体超过上限", e))
		} else {
			fail(problem(400, "invalid_json", "无法读取 JSON", e))
		}
		return
	}
	req := DefaultRequest()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if e = dec.Decode(&req); e != nil {
		fail(problem(400, "invalid_json", "JSON 无效或包含未知字段", e))
		return
	}
	if e = dec.Decode(new(any)); e != io.EOF {
		fail(problem(400, "invalid_json", "只接受一个 JSON 对象", e))
		return
	}
	var fields map[string]json.RawMessage
	if e = json.Unmarshal(data, &fields); e != nil || fields == nil {
		fail(problem(400, "invalid_json", "请求必须是 JSON 对象", e))
		return
	}
	for key, value := range fields {
		if key != "wait_for_selector" && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			fail(invalid(key + " cannot be null"))
			return
		}
	}
	u, e := network.Parse(req.URL)
	if e != nil {
		fail(invalid(e.Error()))
		return
	}
	req.URL = u.String()
	if e = req.Validate(h.Config.MaxOutputChars); e != nil {
		fail(e.(*Error))
		return
	}
	out, e := h.Service.Extract(context.WithValue(r.Context(), requestIDKey{}, id), req)
	if e != nil {
		fail(normalize(e).(*Error))
		return
	}
	out.RequestID = id
	h.Log.Info("extract_complete", "request_id", id, "source", u.Hostname(), "duration_ms", out.DurationMS, "chars", len([]rune(out.Content)), "warnings", out.Warnings)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
}

type requestIDKey struct{}
