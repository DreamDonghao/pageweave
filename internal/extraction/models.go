package extraction

import "fmt"

// Request is the public extraction contract; defaults are applied before decoding.
type Request struct {
	URL             string  `json:"url"`
	Format          string  `json:"format"`
	ContentScope    string  `json:"content_scope"`
	IncludeLinks    bool    `json:"include_links"`
	IncludeImages   bool    `json:"include_images"`
	MaxChars        int     `json:"max_chars"`
	WaitForSelector *string `json:"wait_for_selector"`
	Scroll          bool    `json:"scroll"`
}

// DefaultRequest returns documented defaults and preserves explicit false and zero values.
func DefaultRequest() Request {
	return Request{Format: "markdown", ContentScope: "full", IncludeLinks: true, MaxChars: 12000}
}

// Response contains rendered content and provenance without duplicating the HTML.
type Response struct {
	RequestID    string   `json:"request_id"`
	URL          string   `json:"url"`
	FinalURL     string   `json:"final_url"`
	Title        string   `json:"title"`
	Format       string   `json:"format"`
	ContentScope string   `json:"content_scope"`
	Content      string   `json:"content"`
	Truncated    bool     `json:"truncated"`
	Warnings     []string `json:"warnings"`
	DurationMS   int64    `json:"duration_ms"`
}

// Error carries a public status/code/message and a private diagnostic cause.
type Error struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Cause   error  `json:"-"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }
func (e *Error) Unwrap() error { return e.Cause }
func problem(status int, code, message string, cause error) *Error {
	return &Error{status, code, message, cause}
}
func invalid(message string) *Error { return problem(422, "invalid_request", message, nil) }

// Validate checks output parameters; URL and network checks happen separately.
func (r Request) Validate(limit int) error {
	if r.Format != "markdown" && r.Format != "text" {
		return invalid("format must be markdown or text")
	}
	if r.ContentScope != "main" && r.ContentScope != "full" {
		return invalid("content_scope must be main or full")
	}
	if r.MaxChars < 1 || r.MaxChars > limit {
		return invalid(fmt.Sprintf("max_chars must be 1..%d", limit))
	}
	if r.WaitForSelector != nil && (len([]rune(*r.WaitForSelector)) > 256 || *r.WaitForSelector == "") {
		return invalid("invalid wait_for_selector")
	}
	return nil
}
