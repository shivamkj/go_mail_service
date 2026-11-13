package mailsvc

import (
	"context"
	"fmt"
	"strings"

	"mailsvc/imapclient"
	"mailsvc/message"
	"mailsvc/smtpclient"
	"mailsvc/store"
)

// Service wires together one Store shared by every account, one background
// IMAP IDLE syncer per account, and one pooled SMTP sender per account.
type Service struct {
	Store    *store.Store
	accounts map[string]Account
	smtp     map[string]*smtpclient.Client
}

func New() *Service {
	return &Service{
		Store:    store.New(),
		accounts: make(map[string]Account),
		smtp:     make(map[string]*smtpclient.Client),
	}
}

// AddAccount registers an account, starts its IMAP IDLE sync loop (which
// keeps retrying with backoff for the lifetime of ctx), and readies its
// pooled SMTP sender. Call once per account at startup.
func (s *Service) AddAccount(ctx context.Context, acc Account) {
	s.accounts[acc.ID] = acc

	s.smtp[acc.ID] = smtpclient.New(smtpclient.Config{
		AccountID: acc.ID,
		Host:      acc.SMTP.Host,
		Port:      acc.SMTP.Port,
		Username:  acc.SMTP.Username,
		Password:  acc.SMTP.Password,
		TLS:       acc.SMTP.TLS,
	})

	syncer := imapclient.NewSyncer(imapclient.Config{
		AccountID: acc.ID,
		Host:      acc.IMAP.Host,
		Port:      acc.IMAP.Port,
		Username:  acc.IMAP.Username,
		Password:  acc.IMAP.Password,
		Mailbox:   acc.IMAP.Mailbox,
		TLS:       acc.IMAP.TLS,
	}, s.Store)
	go syncer.Run(ctx)
}

// SendNew composes and sends a brand-new message (no thread ancestry).
func (s *Service) SendNew(accountID string, to, cc []string, subject, text, html string, attachments []message.Attachment) error {
	acc, ok := s.accounts[accountID]
	if !ok {
		return fmt.Errorf("unknown account %q", accountID)
	}
	raw, _, err := smtpclient.Build(smtpclient.Outgoing{
		From: acc.SMTP.From, To: to, Cc: cc,
		Subject: subject, TextBody: text, HTMLBody: html,
		Attachments: attachments,
	})
	if err != nil {
		return err
	}
	return s.smtp[accountID].Send(acc.SMTP.From, append(append([]string{}, to...), cc...), raw)
}

// Reply sends a reply to an existing message, setting In-Reply-To/References
// so providers (Gmail, Outlook, etc.) thread it correctly next to the
// original in every client.
func (s *Service) Reply(accountID string, original *message.Message, text, html string, attachments []message.Attachment) error {
	acc, ok := s.accounts[accountID]
	if !ok {
		return fmt.Errorf("unknown account %q", accountID)
	}
	subject := original.Subject
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}
	raw, _, err := smtpclient.Build(smtpclient.Outgoing{
		From: acc.SMTP.From, To: []string{original.From},
		Subject: subject, TextBody: text, HTMLBody: html,
		Attachments: attachments,
		InReplyTo:   original.MessageID,
		References:  append(append([]string{}, original.References...), original.MessageID),
	})
	if err != nil {
		return err
	}
	return s.smtp[accountID].Send(acc.SMTP.From, []string{original.From}, raw)
}
