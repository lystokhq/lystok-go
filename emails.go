package lystok

import (
	"context"
	"errors"
	"net/http"
)

// EmailsService sends emails and tells what happened to them.
type EmailsService struct {
	client *Client
}

// Send sends one email. It is queued and usually sent within a second.
func (s *EmailsService) Send(ctx context.Context, params *SendEmailParams, opts ...RequestOption) (*EmailCreated, error) {
	if params == nil {
		return nil, errors.New("lystok: Send needs params")
	}
	rc := requestOptions(opts)
	if rc.idempotencyKey == "" {
		rc.idempotencyKey = newUUID()
	}
	var out EmailCreated
	if err := s.client.request(ctx, http.MethodPost, "/emails", params, &out, rc); err != nil {
		return nil, err
	}
	return &out, nil
}

// SendBatch sends up to 100 emails in one request; all are accepted or none. Ids come in the
// same order.
func (s *EmailsService) SendBatch(ctx context.Context, emails []SendEmailParams, opts ...RequestOption) (*EmailBatchCreated, error) {
	rc := requestOptions(opts)
	if rc.idempotencyKey == "" {
		rc.idempotencyKey = newUUID()
	}
	if emails == nil {
		emails = []SendEmailParams{}
	}
	var out EmailBatchCreated
	if err := s.client.request(ctx, http.MethodPost, "/emails/batch", emails, &out, rc); err != nil {
		return nil, err
	}
	return &out, nil
}

// Get tells what happened to an email.
func (s *EmailsService) Get(ctx context.Context, id string, opts ...RequestOption) (*Email, error) {
	var out Email
	if err := s.client.request(ctx, http.MethodGet, "/emails/"+segment(id), nil, &out, requestOptions(opts)); err != nil {
		return nil, err
	}
	return &out, nil
}
