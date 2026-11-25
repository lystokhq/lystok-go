package lystok

// Checking that a webhook came from Lystok (the Standard Webhooks scheme): the headers
// webhook-id, webhook-timestamp and webhook-signature sign "id.timestamp.body" with HMAC-SHA256.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const secretPrefix = "whsec_"

type webhookConfig struct {
	tolerance time.Duration
	now       time.Time
}

// WebhookOption changes how VerifyWebhook checks.
type WebhookOption func(*webhookConfig)

// WithTolerance sets how far the timestamp may be from now. Default 5 minutes.
func WithTolerance(d time.Duration) WebhookOption {
	return func(c *webhookConfig) { c.tolerance = d }
}

// WithNow sets the current time; for tests.
func WithNow(t time.Time) WebhookOption {
	return func(c *webhookConfig) { c.now = t }
}

// VerifyWebhook checks a webhook and returns its event, or a *WebhookVerificationError.
//
// Pass the raw body, exactly as received, before any JSON decoding. secret is the webhook's
// signing secret (whsec_…, from its menu → Signing secret).
func VerifyWebhook(payload []byte, headers http.Header, secret string, opts ...WebhookOption) (*WebhookEvent, error) {
	cfg := webhookConfig{tolerance: 5 * time.Minute}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.now.IsZero() {
		cfg.now = time.Now()
	}
	fail := func(msg string) (*WebhookEvent, error) { return nil, &WebhookVerificationError{Message: msg} }

	id := header(headers, "webhook-id")
	timestamp := header(headers, "webhook-timestamp")
	signatures := header(headers, "webhook-signature")
	if id == "" || timestamp == "" || signatures == "" {
		return fail("Missing webhook-id, webhook-timestamp or webhook-signature header")
	}
	if strings.Trim(timestamp, "0123456789") != "" {
		return fail("Invalid webhook-timestamp header")
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fail("Invalid webhook-timestamp header")
	}
	if diff := cfg.now.Unix() - seconds; diff > int64(cfg.tolerance/time.Second) || -diff > int64(cfg.tolerance/time.Second) {
		return fail("Webhook timestamp is too far from now")
	}

	key, err := base64.StdEncoding.Strict().DecodeString(strings.TrimPrefix(secret, secretPrefix))
	if err != nil || len(key) == 0 {
		return fail("Invalid webhook secret")
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + timestamp + "."))
	mac.Write(payload)
	expected := []byte(base64.StdEncoding.EncodeToString(mac.Sum(nil)))

	for _, entry := range strings.Split(signatures, " ") {
		version, value, _ := strings.Cut(entry, ",")
		if version == "v1" && value != "" && hmac.Equal(expected, []byte(value)) {
			var event WebhookEvent
			if err := json.Unmarshal(payload, &event); err != nil {
				return fail("The webhook body is not a JSON object")
			}
			return &event, nil
		}
	}
	return fail("No matching webhook signature")
}

// header finds a header whether or not its name was canonicalised (an http.Header made by hand
// may have "webhook-id" as a key).
func header(headers http.Header, name string) string {
	values := headers.Values(name)
	if len(values) == 0 {
		for k, v := range headers {
			if strings.EqualFold(k, name) {
				values = v
				break
			}
		}
	}
	return strings.Join(values, " ")
}

// WebhooksService checks webhooks; the same as VerifyWebhook.
type WebhooksService struct{}

// Verify is VerifyWebhook.
func (*WebhooksService) Verify(payload []byte, headers http.Header, secret string, opts ...WebhookOption) (*WebhookEvent, error) {
	return VerifyWebhook(payload, headers, secret, opts...)
}
