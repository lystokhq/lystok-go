package lystok_test

// Against a real Lystok API; skipped unless LYSTOK_LIVE=1. Needs a full-access key and a sender on
// a verified domain of that account:
//
//	LYSTOK_LIVE=1 LYSTOK_API_KEY=lk_... LYSTOK_BASE_URL=http://localhost:3200 \
//	LYSTOK_TEST_FROM=hello@your-domain LYSTOK_TEST_TO=you@your-domain go test ./...

import (
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	lystok "github.com/lystokhq/lystok-go"
)

func live(t *testing.T) (*lystok.Client, func(subject string) lystok.SendEmailParams) {
	t.Helper()
	if os.Getenv("LYSTOK_LIVE") != "1" {
		t.Skip("set LYSTOK_LIVE=1 to run")
	}
	client, err := lystok.New("", lystok.WithBaseURL(os.Getenv("LYSTOK_BASE_URL")))
	if err != nil {
		t.Fatal(err)
	}
	email := func(subject string) lystok.SendEmailParams {
		return lystok.SendEmailParams{From: os.Getenv("LYSTOK_TEST_FROM"), To: os.Getenv("LYSTOK_TEST_TO"), Subject: subject, Text: "lystok-go live test"}
	}
	return client, email
}

func TestLiveSendAndReadBack(t *testing.T) {
	client, email := live(t)
	params := email("lystok-go live test")
	params.Tags = map[string]string{"sdk": "go"}
	sent, err := client.Emails.Send(ctx, &params)
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Emails.Get(ctx, sent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != sent.ID || got.To != os.Getenv("LYSTOK_TEST_TO") || !reflect.DeepEqual(got.Tags, map[string]string{"sdk": "go"}) {
		t.Fatalf("got %+v", got)
	}
}

func TestLiveOncePerIdempotencyKey(t *testing.T) {
	client, email := live(t)
	key := fmt.Sprintf("lystok-go-%d", time.Now().UnixNano())
	params := email("lystok-go idempotency")
	first, err := client.Emails.Send(ctx, &params, lystok.WithIdempotencyKey(key))
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.Emails.Send(ctx, &params, lystok.WithIdempotencyKey(key))
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("%s != %s", first.ID, second.ID)
	}
}

func TestLiveBatch(t *testing.T) {
	client, email := live(t)
	res, err := client.Emails.SendBatch(ctx, []lystok.SendEmailParams{email("lystok-go batch 1"), email("lystok-go batch 2")})
	if err != nil || len(res.Data) != 2 {
		t.Fatalf("got %v, %v", res, err)
	}
}

func TestLiveValidationError(t *testing.T) {
	client, _ := live(t)
	_, err := client.Emails.Send(ctx, &lystok.SendEmailParams{From: os.Getenv("LYSTOK_TEST_FROM"), To: "not-an-address", Subject: "s", Text: "t"})
	if e := asLystokError(t, err); e.Status != 400 || e.Code != "validation" || e.RequestID == "" {
		t.Fatalf("got %+v", e)
	}
}

func TestLiveContact(t *testing.T) {
	client, _ := live(t)
	address := fmt.Sprintf("lystok-go-%d@example.com", time.Now().UnixNano())
	made, err := client.Contacts.Upsert(ctx, &lystok.UpsertContactParams{Email: address, FirstName: "Olena", Tags: []string{"sdk-test"}, ConsentSource: "lystok-go live test"})
	if err != nil {
		t.Fatal(err)
	}
	if made.Status != lystok.ContactSubscribed || !reflect.DeepEqual(made.Tags, []string{"sdk-test"}) {
		t.Fatalf("made %+v", made)
	}
	changed, err := client.Contacts.Update(ctx, address, &lystok.UpdateContactParams{LastName: lystok.Ptr("Shevchenko"), RemoveTags: []string{"sdk-test"}})
	if err != nil {
		t.Fatal(err)
	}
	if changed.LastName == nil || *changed.LastName != "Shevchenko" || len(changed.Tags) != 0 {
		t.Fatalf("changed %+v", changed)
	}
	got, err := client.Contacts.Get(ctx, made.ID)
	if err != nil || got.Email != address {
		t.Fatalf("got %+v, %v", got, err)
	}
	if err := client.Contacts.Delete(ctx, made.ID); err != nil {
		t.Fatal(err)
	}
	_, err = client.Contacts.Get(ctx, address)
	if e := asLystokError(t, err); e.Status != 404 {
		t.Fatalf("got %+v", e)
	}
}
