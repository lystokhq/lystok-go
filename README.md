# Lystok for Go

The official library for the [Lystok](https://lystok.com) email API: send transactional email,
keep contacts in step and check webhooks. Standard library only; Go 1.23+.

```sh
go get github.com/lystokhq/lystok-go
```

## Send an email

Create an API key in Lystok → Settings → For developers, and verify your sender domain.

```go
import lystok "github.com/lystokhq/lystok-go"

client, err := lystok.New(os.Getenv("LYSTOK_API_KEY"))
if err != nil {
	log.Fatal(err)
}

sent, err := client.Emails.Send(ctx, &lystok.SendEmailParams{
	From:    "Shop <orders@shop.example>",
	To:      "olena@example.com",
	Subject: "Your order #1042",
	HTML:    "<p>Thank you! We are packing your order.</p>",
})
if err != nil {
	return err
}
fmt.Println(sent.ID)
```

The key is always passed in: keep it wherever you keep secrets, under any name (`LYSTOK_API_KEY`
above is only an example); the library never reads the environment itself. Keys belong on your
server: never put one in a browser or a mobile app. The client is safe for concurrent use: make one and share it.

### With a template

```go
client.Emails.Send(ctx, &lystok.SendEmailParams{
	From:       "Shop <orders@shop.example>",
	To:         "olena@example.com",
	TemplateID: "019ab3f2-…", // Templates → ⋯ → Copy ID
	Data:       map[string]any{"first_name": "Olena", "order": map[string]any{"number": 1042}},
	Locale:     lystok.LocaleUK,
})
```

### Attachments, tags, tracking

```go
pdf, _ := os.ReadFile("invoice-1042.pdf")

client.Emails.Send(ctx, &lystok.SendEmailParams{
	From:    "Shop <orders@shop.example>",
	To:      "olena@example.com",
	Subject: "Invoice #1042",
	HTML:    "<p>Your invoice is attached.</p>",
	Attachments: []lystok.Attachment{{
		Filename: "invoice-1042.pdf",
		Content:  base64.StdEncoding.EncodeToString(pdf),
	}},
	Tags:     map[string]string{"type": "invoice"},
	Tracking: &lystok.Tracking{Opens: lystok.Ptr(true), Clicks: lystok.Ptr(true)},
})
```

### Many at once, and what happened

```go
res, err := client.Emails.SendBatch(ctx, []lystok.SendEmailParams{
	{From: "Shop <orders@shop.example>", To: "olena@example.com", TemplateID: "…", Data: map[string]any{"first_name": "Olena"}},
	{From: "Shop <orders@shop.example>", To: "taras@example.com", TemplateID: "…", Data: map[string]any{"first_name": "Taras"}},
}) // up to 100; ids in the same order

email, err := client.Emails.Get(ctx, res.Data[0].ID)
email.State // lystok.EmailQueued, EmailSent, EmailDelivered, EmailBounced, EmailComplained, EmailFailed, EmailSuppressed
```

## Sending once

Every send carries an `Idempotency-Key`, so the library's own retries never send an email twice.
To be safe across your own retries too (a crashed job, a repeated webhook), pass your own key —
the same key within 24 hours gets the first answer back:

```go
client.Emails.Send(ctx, email, lystok.WithIdempotencyKey(fmt.Sprintf("order-%d", order.ID)))
```

## Contacts

Needs a key with full access.

```go
contact, err := client.Contacts.Upsert(ctx, &lystok.UpsertContactParams{
	Email:         "olena@example.com",
	FirstName:     "Olena",
	Lists:         []string{"Customers"},
	Tags:          []string{"bought-online"},
	ConsentSource: "Newsletter checkbox at checkout", // required for a new contact
})

client.Contacts.Get(ctx, "olena@example.com") // by address or by id
client.Contacts.Update(ctx, "olena@example.com", &lystok.UpdateContactParams{
	RemoveTags: []string{"bought-online"},
	Fields:     map[string]any{"city": "Lviv"},
})
client.Contacts.Update(ctx, "olena@example.com", &lystok.UpdateContactParams{Status: lystok.ContactUnsubscribed})
client.Contacts.Delete(ctx, contact.ID)
```

In `UpdateContactParams`, nil fields stay as they are: set the ones you change with
`lystok.Ptr("…")`.

## Errors

Every failure of a call is a `*lystok.Error`:

```go
_, err := client.Emails.Send(ctx, email)
var apiErr *lystok.Error
if errors.As(err, &apiErr) {
	apiErr.Status    // 400, 401, 403, 404, 409, 429, 5xx — or 0 when no answer came
	apiErr.Code      // "validation", "not_found", "plan_limit", "access_ended", "timeout", "connection_error", …
	apiErr.Message   // for people
	apiErr.Issues    // []lystok.Issue{{Path: "to", Message: "…"}} for validation errors
	apiErr.RequestID // quote it when you write to support
}
```

When the context is cancelled or its deadline passes, the call returns `ctx.Err()`.

## Retries and timeouts

Network errors, timeouts, `429` and `5xx` are tried again twice, waiting a little longer each time
(or as long as `Retry-After` says). Each try may take 30 seconds; the context bounds them all.

```go
client, err := lystok.New("", lystok.WithMaxRetries(4), lystok.WithTimeout(10*time.Second))
client.Emails.Get(ctx, id, lystok.WithRequestTimeout(5*time.Second))
```

`lystok.WithHTTPClient` sends requests with your own `*http.Client` (a proxy, for example).

## Webhooks

Lystok signs every webhook ([Standard Webhooks](https://www.standardwebhooks.com)). Check the
**raw body** before decoding it; `lystok.VerifyWebhook` returns the event or a
`*lystok.WebhookVerificationError`. The secret (`whsec_…`) is in the webhook's menu → Signing secret.

```go
http.HandleFunc("POST /webhooks/lystok", func(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "", http.StatusBadRequest)
		return
	}
	event, err := lystok.VerifyWebhook(body, r.Header, os.Getenv("LYSTOK_WEBHOOK_SECRET"))
	if err != nil {
		http.Error(w, "", http.StatusUnauthorized)
		return
	}
	if event.Type == lystok.EventEmailBounced {
		var data struct {
			EmailID string `json:"email_id"`
			To      string `json:"to"`
		}
		_ = json.Unmarshal(event.Data, &data)
		// answer within 10 seconds: hand slow work to a queue
	}
	w.WriteHeader(http.StatusOK)
})
```

The same event can arrive twice: skip ones you have handled by the `webhook-id` header.

## Development

```sh
go vet ./... && go test ./...
LYSTOK_LIVE=1 LYSTOK_API_KEY=lk_… LYSTOK_TEST_FROM=… LYSTOK_TEST_TO=… go test ./...   # also against the real API
```

`testdata/webhooks.json` holds webhook signatures made by the Lystok platform itself; the same
file is checked by every Lystok library.

## License

[MIT](LICENSE)
