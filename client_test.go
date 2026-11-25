package lystok_test

// The client against a small local HTTP server that answers from a script.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	lystok "github.com/lystokhq/lystok-go"
)

type answer struct {
	status  int
	body    any
	raw     string
	headers map[string]string
	sleep   time.Duration
}

type call struct {
	method string
	uri    string
	header http.Header
	body   any
}

type fakeAPI struct {
	mu      sync.Mutex
	answers []answer
	calls   []call
	server  *httptest.Server
}

func newFakeAPI(t *testing.T, answers ...answer) *fakeAPI {
	t.Helper()
	api := &fakeAPI{answers: answers}
	api.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body any
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &body)
		}
		api.mu.Lock()
		api.calls = append(api.calls, call{r.Method, r.RequestURI, r.Header.Clone(), body})
		a := answer{status: 200, body: map[string]any{}}
		if len(api.answers) > 0 {
			a, api.answers = api.answers[0], api.answers[1:]
		}
		api.mu.Unlock()
		if a.sleep > 0 {
			select {
			case <-time.After(a.sleep):
			case <-r.Context().Done():
				return
			}
		}
		out := []byte(a.raw)
		if a.raw == "" && a.body != nil {
			out, _ = json.Marshal(a.body)
		}
		for k, v := range a.headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(a.status)
		_, _ = w.Write(out)
	}))
	t.Cleanup(api.server.Close)
	return api
}

func (api *fakeAPI) client(t *testing.T, opts ...lystok.Option) *lystok.Client {
	t.Helper()
	c, err := lystok.New("lk_live_test", append([]lystok.Option{lystok.WithBaseURL(api.server.URL)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (api *fakeAPI) Calls() []call {
	api.mu.Lock()
	defer api.mu.Unlock()
	return append([]call(nil), api.calls...)
}

func apiError(code string) map[string]any {
	return map[string]any{"error": map[string]any{"code": code, "message": "x", "requestId": "req_1"}}
}

func asLystokError(t *testing.T, err error) *lystok.Error {
	t.Helper()
	var apiErr *lystok.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("want a *lystok.Error, got %v", err)
	}
	return apiErr
}

var ctx = context.Background()

func TestRefusesAnEmptyAPIKey(t *testing.T) {
	// The environment is never read, even when it holds a key.
	t.Setenv("LYSTOK_API_KEY", "lk_live_from_env")
	for _, key := range []string{"", "   "} {
		if _, err := lystok.New(key); err == nil || !strings.Contains(err.Error(), "missing API key") {
			t.Fatalf("New(%q): got %v", key, err)
		}
	}
}

func TestSend(t *testing.T) {
	api := newFakeAPI(t, answer{status: 200, body: map[string]any{"id": "019ab3f2"}})
	sent, err := api.client(t).Emails.Send(ctx, &lystok.SendEmailParams{
		From:     "Пекарня <orders@shop.example>",
		To:       "olena@example.com",
		Subject:  "Замовлення",
		HTML:     "<p>Hi</p>",
		Tracking: &lystok.Tracking{Opens: lystok.Ptr(false)},
	})
	if err != nil || sent.ID != "019ab3f2" {
		t.Fatalf("got %v, %v", sent, err)
	}
	c := api.Calls()[0]
	if c.method != "POST" || c.uri != "/v1/emails" {
		t.Fatalf("%s %s", c.method, c.uri)
	}
	want := map[string]any{"from": "Пекарня <orders@shop.example>", "to": "olena@example.com", "subject": "Замовлення", "html": "<p>Hi</p>", "tracking": map[string]any{"opens": false}}
	if !reflect.DeepEqual(c.body, want) {
		t.Fatalf("body %v", c.body)
	}
	for k, v := range map[string]string{"Authorization": "Bearer lk_live_test", "Content-Type": "application/json", "Accept": "application/json", "User-Agent": "lystok-go/" + lystok.Version} {
		if got := c.header.Get(k); got != v {
			t.Fatalf("%s: %q", k, got)
		}
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(c.header.Get("Idempotency-Key")) {
		t.Fatalf("Idempotency-Key %q", c.header.Get("Idempotency-Key"))
	}
}

func TestBatchWithOwnIdempotencyKey(t *testing.T) {
	api := newFakeAPI(t, answer{status: 200, body: map[string]any{"data": []any{map[string]any{"id": "a"}, map[string]any{"id": "b"}}}})
	res, err := api.client(t).Emails.SendBatch(ctx, []lystok.SendEmailParams{{From: "a@x.example", To: "b@x.example", TemplateID: "t"}}, lystok.WithIdempotencyKey("order-1042"))
	if err != nil || len(res.Data) != 2 || res.Data[1].ID != "b" {
		t.Fatalf("got %v, %v", res, err)
	}
	c := api.Calls()[0]
	if c.uri != "/v1/emails/batch" || c.header.Get("Idempotency-Key") != "order-1042" {
		t.Fatalf("%s %q", c.uri, c.header.Get("Idempotency-Key"))
	}
	if _, isList := c.body.([]any); !isList {
		t.Fatalf("body %v is not a list", c.body)
	}
}

func TestContacts(t *testing.T) {
	contact := map[string]any{"id": "c1", "email": "olena@example.com", "first_name": nil, "status": "subscribed", "tags": []string{}, "created_at": "2026-11-24T10:15:02.120Z"}
	api := newFakeAPI(t,
		answer{status: 201, body: contact},
		answer{status: 200, body: contact},
		answer{status: 200, body: contact},
		answer{status: 204},
	)
	c := api.client(t)
	made, err := c.Contacts.Upsert(ctx, &lystok.UpsertContactParams{Email: "olena@example.com", ConsentSource: "Checkout"})
	if err != nil || made.ID != "c1" || made.FirstName != nil || made.Status != lystok.ContactSubscribed || made.CreatedAt.IsZero() {
		t.Fatalf("got %+v, %v", made, err)
	}
	if _, err := c.Contacts.Get(ctx, "olena+shop@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Contacts.Update(ctx, "c1", &lystok.UpdateContactParams{RemoveTags: []string{"old"}, Status: lystok.ContactUnsubscribed, ClearLocale: true}); err != nil {
		t.Fatal(err)
	}
	if err := c.Contacts.Delete(ctx, "c1"); err != nil {
		t.Fatal(err)
	}
	calls := api.Calls()
	var got []string
	for _, c := range calls {
		got = append(got, c.method+" "+c.uri)
	}
	want := []string{"POST /v1/contacts", "GET /v1/contacts/olena%2Bshop%40example.com", "PATCH /v1/contacts/c1", "DELETE /v1/contacts/c1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("calls %v", got)
	}
	if calls[0].header.Get("Idempotency-Key") != "" {
		t.Fatal("an Idempotency-Key on contacts")
	}
	if !reflect.DeepEqual(calls[2].body, map[string]any{"remove_tags": []any{"old"}, "status": "unsubscribed", "locale": nil}) {
		t.Fatalf("update body %v", calls[2].body)
	}
}

func TestErrorAnswer(t *testing.T) {
	api := newFakeAPI(t, answer{status: 400, body: map[string]any{"error": map[string]any{
		"code":      "validation",
		"message":   "The request is not valid. See `issues` for the fields.",
		"issues":    []any{map[string]any{"path": "to", "message": "Invalid email"}},
		"requestId": "req_1",
	}}})
	_, err := api.client(t).Emails.Send(ctx, &lystok.SendEmailParams{From: "a@x.example", To: "nope"})
	e := asLystokError(t, err)
	if e.Status != 400 || e.Code != "validation" || e.RequestID != "req_1" || !strings.Contains(e.Error(), "not valid") {
		t.Fatalf("got %+v", e)
	}
	if !reflect.DeepEqual(e.Issues, []lystok.Issue{{Path: "to", Message: "Invalid email"}}) {
		t.Fatalf("issues %v", e.Issues)
	}
	if n := len(api.Calls()); n != 1 {
		t.Fatalf("a refused request was tried %d times", n)
	}
}

func TestAnswerThatIsNotJSON(t *testing.T) {
	api := newFakeAPI(t, answer{status: 502, raw: "<html>Bad gateway</html>", headers: map[string]string{"X-Request-Id": "req_9"}})
	_, err := api.client(t, lystok.WithMaxRetries(0)).Emails.Get(ctx, "e1")
	e := asLystokError(t, err)
	if e.Status != 502 || e.Code != "server_error" || e.RequestID != "req_9" {
		t.Fatalf("got %+v", e)
	}
}

func TestRetry503WithTheSameKey(t *testing.T) {
	api := newFakeAPI(t, answer{status: 503, body: apiError("unavailable")}, answer{status: 200, body: map[string]any{"id": "e1"}})
	sent, err := api.client(t).Emails.Send(ctx, &lystok.SendEmailParams{From: "a@x.example", To: "b@x.example", Text: "t"})
	if err != nil || sent.ID != "e1" {
		t.Fatalf("got %v, %v", sent, err)
	}
	calls := api.Calls()
	if len(calls) != 2 || calls[0].header.Get("Idempotency-Key") != calls[1].header.Get("Idempotency-Key") {
		t.Fatalf("%d calls, keys differ or missing", len(calls))
	}
}

func TestRetry429WaitsRetryAfter(t *testing.T) {
	api := newFakeAPI(t,
		answer{status: 429, body: apiError("rate_limited"), headers: map[string]string{"Retry-After": "1"}},
		answer{status: 200, body: map[string]any{"id": "e1"}},
	)
	started := time.Now()
	if _, err := api.client(t).Emails.Get(ctx, "e1"); err != nil {
		t.Fatal(err)
	}
	if waited := time.Since(started); waited < 950*time.Millisecond {
		t.Fatalf("waited only %s", waited)
	}
}

func TestRetryIdempotencyInProgress(t *testing.T) {
	api := newFakeAPI(t, answer{status: 409, body: apiError("idempotency_in_progress")}, answer{status: 200, body: map[string]any{"id": "e1"}})
	if _, err := api.client(t).Emails.Send(ctx, &lystok.SendEmailParams{From: "a@x.example", To: "b@x.example", Text: "t"}); err != nil {
		t.Fatal(err)
	}
}

func TestNoRetryAfterIdempotencyMismatch(t *testing.T) {
	api := newFakeAPI(t, answer{status: 409, body: apiError("idempotency_mismatch")})
	_, err := api.client(t).Emails.Send(ctx, &lystok.SendEmailParams{From: "a@x.example", To: "b@x.example", Text: "t"})
	if e := asLystokError(t, err); e.Code != "idempotency_mismatch" || len(api.Calls()) != 1 {
		t.Fatalf("got %+v", e)
	}
}

func TestTimeout(t *testing.T) {
	api := newFakeAPI(t, answer{status: 200, body: map[string]any{"id": "e1"}, sleep: 2 * time.Second})
	_, err := api.client(t, lystok.WithTimeout(100*time.Millisecond), lystok.WithMaxRetries(0)).Emails.Get(ctx, "e1")
	if e := asLystokError(t, err); e.Status != 0 || e.Code != "timeout" {
		t.Fatalf("got %+v", e)
	}
}

func TestConnectionError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close() // nothing listens there now
	c, _ := lystok.New("lk_live_test", lystok.WithBaseURL("http://"+addr), lystok.WithMaxRetries(1))
	_, err = c.Emails.Get(ctx, "e1")
	if e := asLystokError(t, err); e.Status != 0 || e.Code != "connection_error" || e.Unwrap() == nil {
		t.Fatalf("got %+v", e)
	}
}

func TestCanceledContext(t *testing.T) {
	api := newFakeAPI(t, answer{status: 503, body: apiError("unavailable")})
	cctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	_, err := api.client(t, lystok.WithMaxRetries(5)).Emails.Get(cctx, "e1")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
}

func TestDeleteThatWentThroughBeforeAFailure(t *testing.T) {
	api := newFakeAPI(t, answer{status: 503, body: apiError("unavailable")}, answer{status: 404, body: apiError("not_found")})
	if err := api.client(t).Contacts.Delete(ctx, "c1"); err != nil {
		t.Fatal(err)
	}
	if n := len(api.Calls()); n != 2 {
		t.Fatalf("%d calls", n)
	}
}

func TestDelete404OnTheFirstTry(t *testing.T) {
	api := newFakeAPI(t, answer{status: 404, body: apiError("not_found")})
	err := api.client(t).Contacts.Delete(ctx, "c1")
	if e := asLystokError(t, err); e.Status != 404 {
		t.Fatalf("got %+v", e)
	}
}
