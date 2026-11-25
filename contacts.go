package lystok

import (
	"context"
	"errors"
	"net/http"
)

// ContactsService keeps your audience in step. Needs a key with full access.
type ContactsService struct {
	client *Client
}

// Upsert adds a person, or updates them when the address is already there. Safe to repeat.
func (s *ContactsService) Upsert(ctx context.Context, params *UpsertContactParams, opts ...RequestOption) (*Contact, error) {
	if params == nil {
		return nil, errors.New("lystok: Upsert needs params")
	}
	return s.contact(ctx, http.MethodPost, "/contacts", params, opts)
}

// Get finds a person by id or by email address.
func (s *ContactsService) Get(ctx context.Context, idOrEmail string, opts ...RequestOption) (*Contact, error) {
	return s.contact(ctx, http.MethodGet, "/contacts/"+segment(idOrEmail), nil, opts)
}

// Update changes a person, found by id or by email address.
func (s *ContactsService) Update(ctx context.Context, idOrEmail string, params *UpdateContactParams, opts ...RequestOption) (*Contact, error) {
	if params == nil {
		params = &UpdateContactParams{}
	}
	return s.contact(ctx, http.MethodPatch, "/contacts/"+segment(idOrEmail), params, opts)
}

// Delete deletes a person, found by id or by email address.
func (s *ContactsService) Delete(ctx context.Context, idOrEmail string, opts ...RequestOption) error {
	return s.client.request(ctx, http.MethodDelete, "/contacts/"+segment(idOrEmail), nil, nil, requestOptions(opts))
}

func (s *ContactsService) contact(ctx context.Context, method, path string, body any, opts []RequestOption) (*Contact, error) {
	var out Contact
	if err := s.client.request(ctx, method, path, body, &out, requestOptions(opts)); err != nil {
		return nil, err
	}
	return &out, nil
}
