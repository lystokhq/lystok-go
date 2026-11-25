package lystok_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"reflect"
	"testing"
	"time"

	lystok "github.com/lystokhq/lystok-go"
)

// Signed by the Lystok platform itself, so passing them means accepting exactly what it sends.
type webhookCase struct {
	Name    string            `json:"name"`
	Secret  string            `json:"secret"`
	Payload string            `json:"payload"`
	Headers map[string]string `json:"headers"`
	Now     int64             `json:"now"`
	Valid   bool              `json:"valid"`
}

func webhookCases(t *testing.T) []webhookCase {
	t.Helper()
	raw, err := os.ReadFile("testdata/webhooks.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures struct {
		Cases []webhookCase `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	return fixtures.Cases
}

func validCase(t *testing.T) webhookCase {
	for _, c := range webhookCases(t) {
		if c.Name == "valid" {
			return c
		}
	}
	t.Fatal("no valid case")
	return webhookCase{}
}

func headersOf(m map[string]string) http.Header {
	h := http.Header{}
	for k, v := range m {
		h.Set(k, v)
	}
	return h
}

func TestWebhookPlatformCases(t *testing.T) {
	for _, c := range webhookCases(t) {
		t.Run(c.Name, func(t *testing.T) {
			event, err := lystok.VerifyWebhook([]byte(c.Payload), headersOf(c.Headers), c.Secret, lystok.WithNow(time.Unix(c.Now, 0)))
			if !c.Valid {
				var verr *lystok.WebhookVerificationError
				if !errors.As(err, &verr) {
					t.Fatalf("want a WebhookVerificationError, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var want struct {
				Type string `json:"type"`
				Data any    `json:"data"`
			}
			if err := json.Unmarshal([]byte(c.Payload), &want); err != nil {
				t.Fatal(err)
			}
			var data any
			if err := json.Unmarshal(event.Data, &data); err != nil {
				t.Fatal(err)
			}
			if string(event.Type) != want.Type || !reflect.DeepEqual(data, want.Data) || event.CreatedAt.IsZero() {
				t.Fatalf("event %+v does not match the payload", event)
			}
		})
	}
}

func TestWebhookHeadersByHand(t *testing.T) {
	c := validCase(t)
	// Keys that were never canonicalised, as in http.Header{"webhook-id": …}.
	raw := http.Header{}
	for k, v := range c.Headers {
		raw[k] = []string{v}
	}
	event, err := lystok.VerifyWebhook([]byte(c.Payload), raw, c.Secret, lystok.WithNow(time.Unix(c.Now, 0)))
	if err != nil || event.Type != lystok.EventEmailDelivered {
		t.Fatalf("got %v, %v", event, err)
	}
}

func TestWebhookTolerance(t *testing.T) {
	c := validCase(t)
	_, err := lystok.VerifyWebhook([]byte(c.Payload), headersOf(c.Headers), c.Secret, lystok.WithNow(time.Unix(c.Now+120, 0)), lystok.WithTolerance(time.Minute))
	if err == nil {
		t.Fatal("2 minutes late passed a 1 minute tolerance")
	}
}

func TestWebhookSecretThatIsNotBase64(t *testing.T) {
	c := validCase(t)
	_, err := lystok.VerifyWebhook([]byte(c.Payload), headersOf(c.Headers), "whsec_not base64!", lystok.WithNow(time.Unix(c.Now, 0)))
	var verr *lystok.WebhookVerificationError
	if !errors.As(err, &verr) {
		t.Fatalf("want a WebhookVerificationError, got %v", err)
	}
}

func TestWebhookOnTheClient(t *testing.T) {
	c := validCase(t)
	client, err := lystok.New("lk_test_x")
	if err != nil {
		t.Fatal(err)
	}
	event, err := client.Webhooks.Verify([]byte(c.Payload), headersOf(c.Headers), c.Secret, lystok.WithNow(time.Unix(c.Now, 0)))
	if err != nil || event.Type != lystok.EventEmailDelivered {
		t.Fatalf("got %v, %v", event, err)
	}
}
