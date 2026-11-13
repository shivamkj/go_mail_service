package imapclient

import (
	"io"
	"strings"

	_ "github.com/emersion/go-message/charset"
	emmail "github.com/emersion/go-message/mail"

	"mailsvc/message"
)

func parseMessage(accountID, mailbox string, uid uint32, flags []string, r io.Reader) (*message.Message, error) {
	mr, err := emmail.CreateReader(r)
	if err != nil {
		return nil, err
	}

	h := mr.Header
	subject, _ := h.Subject()
	date, _ := h.Date()
	from, _ := h.AddressList("From")
	to, _ := h.AddressList("To")
	cc, _ := h.AddressList("Cc")

	m := &message.Message{
		AccountID:  accountID,
		Mailbox:    mailbox,
		UID:        uid,
		MessageID:  trimBrackets(h.Get("Message-Id")),
		InReplyTo:  trimBrackets(h.Get("In-Reply-To")),
		References: splitRefs(h.Get("References")),
		Subject:    subject,
		Date:       date,
		Flags:      flags,
	}
	if len(from) > 0 {
		m.From = from[0].Address
	}
	for _, a := range to {
		m.To = append(m.To, a.Address)
	}
	for _, a := range cc {
		m.Cc = append(m.Cc, a.Address)
	}

	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch ph := p.Header.(type) {
		case *emmail.InlineHeader:
			ct, _, _ := ph.ContentType()
			b, _ := io.ReadAll(p.Body)
			if strings.HasPrefix(ct, "text/html") {
				m.HTMLBody += string(b)
			} else {
				m.TextBody += string(b)
			}
		case *emmail.AttachmentHeader:
			filename, _ := ph.Filename()
			ct, _, _ := ph.ContentType()
			b, _ := io.ReadAll(p.Body)
			m.Attachments = append(m.Attachments, message.Attachment{
				Filename:    filename,
				ContentType: ct,
				Data:        b,
			})
		}
	}
	return m, nil
}

func trimBrackets(s string) string {
	return strings.Trim(strings.TrimSpace(s), "<>")
}

func splitRefs(raw string) []string {
	fields := strings.Fields(raw)
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		out = append(out, trimBrackets(f))
	}
	return out
}
