// Package message defines the data shapes shared by the IMAP syncer, the
// SMTP sender, and the in-memory store.
package message

import "time"

type Attachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// Message is a normalized view of one email, independent of provider.
type Message struct {
	AccountID string
	Mailbox   string
	UID       uint32

	MessageID  string   // RFC 5322 Message-ID, without angle brackets
	InReplyTo  string   // parent Message-ID, if any
	References []string // full ancestry chain, root first
	ThreadID   string   // computed by store.Store on Upsert

	From    string
	To      []string
	Cc      []string
	Subject string
	Date    time.Time

	TextBody    string
	HTMLBody    string
	Attachments []Attachment
	Flags       []string
}
