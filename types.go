package lystok

import (
	"encoding/json"
	"time"
)

// Locale is a language of Lystok templates and pages.
type Locale string

const (
	LocaleUK Locale = "uk"
	LocaleEN Locale = "en"
	LocalePL Locale = "pl"
	LocaleDE Locale = "de"
	LocaleES Locale = "es"
	LocaleFR Locale = "fr"
	LocaleRU Locale = "ru"
)

// Attachment is a file sent with an email.
type Attachment struct {
	Filename string `json:"filename"`
	// The file, base64-encoded.
	Content     string `json:"content"`
	ContentType string `json:"content_type,omitempty"`
}

// Tracking turns open and click tracking on or off for one email; nil keeps the account's setting.
type Tracking struct {
	Opens  *bool `json:"opens,omitempty"`
	Clicks *bool `json:"clicks,omitempty"`
}

// SendEmailParams is one email. Give Subject with HTML or Text, or TemplateID with Data.
type SendEmailParams struct {
	// "Name <address@your-domain>"; the domain must be verified.
	From    string `json:"from"`
	To      string `json:"to"`
	Subject string `json:"subject,omitempty"`
	HTML    string `json:"html,omitempty"`
	Text    string `json:"text,omitempty"`
	// Templates → ⋯ → Copy ID.
	TemplateID string `json:"template_id,omitempty"`
	// Values for the template's {{variables}}.
	Data        map[string]any    `json:"data,omitempty"`
	Locale      Locale            `json:"locale,omitempty"`
	ReplyTo     string            `json:"reply_to,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	Tracking    *Tracking         `json:"tracking,omitempty"`
	Attachments []Attachment      `json:"attachments,omitempty"`
}

// EmailCreated is the answer to a send.
type EmailCreated struct {
	ID string `json:"id"`
}

// EmailBatchCreated is the answer to a batch send; ids come in the order of the emails.
type EmailBatchCreated struct {
	Data []EmailCreated `json:"data"`
}

// EmailState is where an email is: queued → sent → delivered; or bounced, complained, failed,
// suppressed.
type EmailState string

const (
	EmailQueued     EmailState = "queued"
	EmailSent       EmailState = "sent"
	EmailDelivered  EmailState = "delivered"
	EmailBounced    EmailState = "bounced"
	EmailComplained EmailState = "complained"
	EmailFailed     EmailState = "failed"
	EmailSuppressed EmailState = "suppressed"
)

// Email is what happened to an email.
type Email struct {
	ID         string     `json:"id"`
	From       string     `json:"from"`
	To         string     `json:"to"`
	Subject    string     `json:"subject"`
	TemplateID *string    `json:"template_id"`
	State      EmailState `json:"state"`
	// Why it failed or was suppressed.
	Error        *string           `json:"error"`
	Tags         map[string]string `json:"tags"`
	CreatedAt    time.Time         `json:"created_at"`
	SentAt       *time.Time        `json:"sent_at"`
	DeliveredAt  *time.Time        `json:"delivered_at"`
	BouncedAt    *time.Time        `json:"bounced_at"`
	ComplainedAt *time.Time        `json:"complained_at"`
	OpenedAt     *time.Time        `json:"opened_at"`
	ClickedAt    *time.Time        `json:"clicked_at"`
}

// ContactStatus says whether a person gets your newsletters.
type ContactStatus string

const (
	ContactSubscribed   ContactStatus = "subscribed"
	ContactUnsubscribed ContactStatus = "unsubscribed"
	ContactBounced      ContactStatus = "bounced"
	ContactComplained   ContactStatus = "complained"
)

// Contact is a person in your audience.
type Contact struct {
	ID        string        `json:"id"`
	Email     string        `json:"email"`
	FirstName *string       `json:"first_name"`
	LastName  *string       `json:"last_name"`
	Phone     *string       `json:"phone"`
	Locale    *string       `json:"locale"`
	Status    ContactStatus `json:"status"`
	// Your own fields by key: strings, numbers and booleans.
	Fields map[string]any `json:"fields"`
	Lists  []string       `json:"lists"`
	Tags   []string       `json:"tags"`
	// How the contact was added: manual, import, api, form.
	Source        string     `json:"source"`
	ConsentSource *string    `json:"consent_source"`
	ConsentAt     *time.Time `json:"consent_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// UpsertContactParams adds a person, or updates them when the address is already there.
type UpsertContactParams struct {
	Email     string `json:"email"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	Phone     string `json:"phone,omitempty"`
	Locale    Locale `json:"locale,omitempty"`
	// Your own fields by key; a nil value removes it.
	Fields map[string]any `json:"fields,omitempty"`
	Lists  []string       `json:"lists,omitempty"`
	Tags   []string       `json:"tags,omitempty"`
	// How the person agreed to get your emails. Required for a new contact.
	ConsentSource string `json:"consent_source,omitempty"`
}

// UpdateContactParams changes a person; nil fields stay as they are.
type UpdateContactParams struct {
	Email     *string `json:"email,omitempty"`
	FirstName *string `json:"first_name,omitempty"`
	LastName  *string `json:"last_name,omitempty"`
	Phone     *string `json:"phone,omitempty"`
	Locale    *Locale `json:"locale,omitempty"`
	// ClearLocale removes the person's language, so they get your default one.
	ClearLocale bool `json:"-"`
	// Your own fields by key; a nil value removes it.
	Fields        map[string]any `json:"fields,omitempty"`
	Lists         []string       `json:"lists,omitempty"`
	Tags          []string       `json:"tags,omitempty"`
	ConsentSource *string        `json:"consent_source,omitempty"`
	RemoveLists   []string       `json:"remove_lists,omitempty"`
	RemoveTags    []string       `json:"remove_tags,omitempty"`
	// ContactUnsubscribed unsubscribes the person from your newsletters.
	Status ContactStatus `json:"status,omitempty"`
}

// MarshalJSON sends "locale": null when ClearLocale is set.
func (p UpdateContactParams) MarshalJSON() ([]byte, error) {
	type plain UpdateContactParams
	b, err := json.Marshal(plain(p))
	if err != nil || !p.ClearLocale || p.Locale != nil {
		return b, err
	}
	if len(b) == 2 { // {}
		return []byte(`{"locale":null}`), nil
	}
	return append([]byte(`{"locale":null,`), b[1:]...), nil
}

// WebhookEventType is what a webhook tells about.
type WebhookEventType string

const (
	EventEmailSent           WebhookEventType = "email.sent"
	EventEmailDelivered      WebhookEventType = "email.delivered"
	EventEmailBounced        WebhookEventType = "email.bounced"
	EventEmailComplained     WebhookEventType = "email.complained"
	EventEmailFailed         WebhookEventType = "email.failed"
	EventEmailSuppressed     WebhookEventType = "email.suppressed"
	EventEmailOpened         WebhookEventType = "email.opened"
	EventEmailClicked        WebhookEventType = "email.clicked"
	EventContactUnsubscribed WebhookEventType = "contact.unsubscribed"
)

// WebhookEvent is the body of a webhook. Decode Data into a struct or a map of your own.
type WebhookEvent struct {
	Type      WebhookEventType `json:"type"`
	CreatedAt time.Time        `json:"created_at"`
	Data      json.RawMessage  `json:"data"`
}
