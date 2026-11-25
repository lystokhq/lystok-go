// Package lystok is the official Go library for the Lystok email API: send transactional email,
// keep contacts in step and check webhooks.
//
//	client, err := lystok.New(os.Getenv("LYSTOK_API_KEY")) // your key, from wherever you keep it
//	if err != nil {
//		log.Fatal(err)
//	}
//	sent, err := client.Emails.Send(ctx, &lystok.SendEmailParams{
//		From:    "Shop <orders@shop.example>",
//		To:      "olena@example.com",
//		Subject: "Your order #1042",
//		HTML:    "<p>Thank you!</p>",
//	})
package lystok

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Version of this library, sent in the User-Agent header.
const Version = "0.1.0"

const defaultBaseURL = "https://api.lystok.com"

// Client talks to the Lystok API. It is safe for concurrent use.
type Client struct {
	Emails   *EmailsService
	Contacts *ContactsService
	Webhooks *WebhooksService

	apiKey     string
	baseURL    string
	timeout    time.Duration
	maxRetries int
	httpClient *http.Client
}

// Option changes a Client made by New.
type Option func(*Client)

// WithBaseURL sets the API address. Default https://api.lystok.com.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		if baseURL != "" {
			c.baseURL = strings.TrimRight(baseURL, "/")
		}
	}
}

// WithTimeout sets how long one attempt may wait for the API. Default 30 seconds.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.timeout = d
		}
	}
}

// WithMaxRetries sets how many times to try again after a network error, a timeout, 429 or 5xx.
// Default 2. Safe for emails too: every send carries an Idempotency-Key.
func WithMaxRetries(n int) Option {
	return func(c *Client) { c.maxRetries = max(0, n) }
}

// WithHTTPClient sets the *http.Client to send requests with, for proxies or tests.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// New makes a client. apiKey is an API key from Settings → For developers (lk_live_…); New
// returns an error when it is empty. Read it from wherever you keep secrets: the library itself
// never looks at the environment.
func New(apiKey string, opts ...Option) (*Client, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("lystok: missing API key: pass your Lystok API key (lk_live_…) to lystok.New")
	}
	c := &Client{
		apiKey:     apiKey,
		baseURL:    defaultBaseURL,
		timeout:    30 * time.Second,
		maxRetries: 2,
		httpClient: &http.Client{},
	}
	for _, opt := range opts {
		opt(c)
	}
	c.Emails = &EmailsService{client: c}
	c.Contacts = &ContactsService{client: c}
	c.Webhooks = &WebhooksService{}
	return c, nil
}

// RequestOption changes one call.
type RequestOption func(*requestConfig)

type requestConfig struct {
	idempotencyKey string
	timeout        time.Duration
}

// WithIdempotencyKey sets the Idempotency-Key of an email send. The same key within 24 hours
// gets the first answer back instead of a second email: use your order id, for example. Made up
// for you when not given, which protects the call's own retries.
func WithIdempotencyKey(key string) RequestOption {
	return func(rc *requestConfig) { rc.idempotencyKey = key }
}

// WithRequestTimeout sets how long each attempt of this call may wait, instead of the client's
// timeout. Use the context for a deadline over all attempts.
func WithRequestTimeout(d time.Duration) RequestOption {
	return func(rc *requestConfig) { rc.timeout = d }
}

func requestOptions(opts []RequestOption) requestConfig {
	var rc requestConfig
	for _, opt := range opts {
		opt(&rc)
	}
	return rc
}

func (c *Client) request(ctx context.Context, method, path string, body, out any, rc requestConfig) error {
	var data []byte
	if body != nil {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(body); err != nil {
			return fmt.Errorf("lystok: encoding the request: %w", err)
		}
		data = bytes.TrimRight(buf.Bytes(), "\n")
	}
	timeout := c.timeout
	if rc.timeout > 0 {
		timeout = rc.timeout
	}

	for attempt := 0; ; attempt++ {
		var wait time.Duration
		status, header, raw, noAnswer := c.send(ctx, method, c.baseURL+"/v1"+path, data, rc.idempotencyKey, timeout)
		if noAnswer != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if attempt >= c.maxRetries {
				return noAnswer
			}
			wait = backoff(attempt)
		} else {
			if status < 300 {
				if out == nil || len(bytes.TrimSpace(raw)) == 0 {
					return nil
				}
				if err := json.Unmarshal(raw, out); err != nil {
					return fmt.Errorf("lystok: reading the answer: %w", err)
				}
				return nil
			}
			// A DELETE whose first try went through before the network failed finds nothing.
			if status == http.StatusNotFound && method == http.MethodDelete && attempt > 0 {
				return nil
			}
			apiErr := errorFrom(status, header, raw)
			retry := retryStatus(status) || apiErr.Code == "idempotency_in_progress"
			if !retry || attempt >= c.maxRetries {
				return apiErr
			}
			wait = backoff(attempt)
			if apiErr.RetryAfter > 0 {
				wait = min(apiErr.RetryAfter, 60*time.Second)
			}
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// send makes one attempt. A nil *Error means an answer came, whatever its status.
func (c *Client) send(ctx context.Context, method, url string, data []byte, idempotencyKey string, timeout time.Duration) (int, http.Header, []byte, *Error) {
	attemptCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var body io.Reader
	if data != nil {
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(attemptCtx, method, url, body)
	if err != nil {
		return 0, nil, nil, &Error{Code: "connection_error", Message: "Could not reach the Lystok API: " + err.Error(), cause: err}
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "lystok-go/"+Version)
	if data != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	res, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, nil, noAnswer(attemptCtx, ctx, timeout, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return 0, nil, nil, noAnswer(attemptCtx, ctx, timeout, err)
	}
	return res.StatusCode, res.Header, raw, nil
}

func noAnswer(attemptCtx, ctx context.Context, timeout time.Duration, err error) *Error {
	var netErr net.Error
	timedOut := ctx.Err() == nil && errors.Is(attemptCtx.Err(), context.DeadlineExceeded)
	if timedOut || (errors.As(err, &netErr) && netErr.Timeout()) {
		return &Error{Code: "timeout", Message: fmt.Sprintf("No answer from the Lystok API within %s", timeout), cause: err}
	}
	return &Error{Code: "connection_error", Message: "Could not reach the Lystok API: " + err.Error(), cause: err}
}

func errorFrom(status int, header http.Header, raw []byte) *Error {
	var parsed struct {
		Error struct {
			Code      string         `json:"code"`
			Message   string         `json:"message"`
			Params    map[string]any `json:"params"`
			Issues    []Issue        `json:"issues"`
			RequestID string         `json:"requestId"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &parsed) // an answer that is not JSON keeps the defaults below
	e := &Error{
		Status:    status,
		Code:      parsed.Error.Code,
		Message:   parsed.Error.Message,
		Params:    parsed.Error.Params,
		Issues:    parsed.Error.Issues,
		RequestID: parsed.Error.RequestID,
	}
	if e.Code == "" {
		e.Code = "http_error"
		if status >= 500 {
			e.Code = "server_error"
		}
	}
	if e.Message == "" {
		e.Message = fmt.Sprintf("The Lystok API answered %d", status)
	}
	if e.RequestID == "" {
		e.RequestID = header.Get("X-Request-Id")
	}
	if s, err := strconv.ParseFloat(header.Get("Retry-After"), 64); err == nil && s > 0 {
		e.RetryAfter = time.Duration(s * float64(time.Second))
	}
	return e
}

func retryStatus(status int) bool {
	switch status {
	case 408, 429, 500, 502, 503, 504:
		return true
	}
	return false
}

func backoff(attempt int) time.Duration {
	seconds := min(0.5*math.Pow(2, float64(attempt)), 8) * (0.75 + rand.Float64()*0.5)
	return time.Duration(seconds * float64(time.Second))
}

func newUUID() string {
	var b [16]byte
	_, _ = crand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// segment escapes an id or an email address for a URL path, like encodeURIComponent.
func segment(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_' || ch == '.' || ch == '~' {
			b.WriteByte(ch)
		} else {
			b.WriteByte('%')
			b.WriteByte(hex[ch>>4])
			b.WriteByte(hex[ch&15])
		}
	}
	return b.String()
}

// Ptr returns a pointer to v, for the optional fields of UpdateContactParams and Tracking.
func Ptr[T any](v T) *T { return &v }
