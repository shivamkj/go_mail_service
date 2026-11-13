package smtpclient

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	emmail "github.com/emersion/go-message/mail"

	"mailsvc/message"
)

// Outgoing describes a message to send. Leave InReplyTo/References empty for
// a brand-new message; Service.Reply fills them in to thread a reply.
type Outgoing struct {
	From        string
	To          []string
	Cc          []string
	Subject     string
	TextBody    string
	HTMLBody    string
	Attachments []message.Attachment
	InReplyTo   string
	References  []string
}

// Build renders an Outgoing message into raw RFC822 bytes ready for
// Pool.Send, and returns the Message-ID it generated.
func Build(o Outgoing) (raw []byte, messageID string, err error) {
	var buf bytes.Buffer

	var h emmail.Header
	h.SetDate(time.Now())
	h.SetAddressList("From", addrs([]string{o.From}))
	h.SetAddressList("To", addrs(o.To))
	if len(o.Cc) > 0 {
		h.SetAddressList("Cc", addrs(o.Cc))
	}
	h.SetSubject(o.Subject)

	messageID = genMessageID(o.From)
	h.Set("Message-Id", "<"+messageID+">")
	if o.InReplyTo != "" {
		h.Set("In-Reply-To", "<"+o.InReplyTo+">")
	}
	if len(o.References) > 0 {
		var refs strings.Builder
		for _, r := range o.References {
			fmt.Fprintf(&refs, "<%s> ", r)
		}
		h.Set("References", strings.TrimSpace(refs.String()))
	}

	mw, err := emmail.CreateWriter(&buf, h)
	if err != nil {
		return nil, "", fmt.Errorf("create writer: %w", err)
	}

	if err := writeBody(mw, o.TextBody, o.HTMLBody); err != nil {
		return nil, "", fmt.Errorf("write body: %w", err)
	}
	if err := writeAttachments(mw, o.Attachments); err != nil {
		return nil, "", fmt.Errorf("write attachments: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), messageID, nil
}

func writeBody(mw *emmail.Writer, text, html string) error {
	tw, err := mw.CreateInline()
	if err != nil {
		return err
	}
	defer tw.Close()

	var th emmail.InlineHeader
	th.Set("Content-Type", "text/plain; charset=utf-8")
	pw, err := tw.CreatePart(th)
	if err != nil {
		return err
	}
	io.WriteString(pw, text)
	pw.Close()

	if html != "" {
		var hh emmail.InlineHeader
		hh.Set("Content-Type", "text/html; charset=utf-8")
		hw, err := tw.CreatePart(hh)
		if err != nil {
			return err
		}
		io.WriteString(hw, html)
		hw.Close()
	}
	return nil
}

func writeAttachments(mw *emmail.Writer, attachments []message.Attachment) error {
	for _, a := range attachments {
		var ah emmail.AttachmentHeader
		ct := a.ContentType
		if ct == "" {
			ct = mime.TypeByExtension(filepath.Ext(a.Filename))
		}
		if ct == "" {
			ct = "application/octet-stream"
		}
		ah.Set("Content-Type", ct)
		ah.SetFilename(a.Filename)

		aw, err := mw.CreateAttachment(ah)
		if err != nil {
			return err
		}
		if _, err := aw.Write(a.Data); err != nil {
			aw.Close()
			return err
		}
		aw.Close()
	}
	return nil
}

func addrs(list []string) []*emmail.Address {
	out := make([]*emmail.Address, 0, len(list))
	for _, a := range list {
		if a != "" {
			out = append(out, &emmail.Address{Address: a})
		}
	}
	return out
}

var idCounter uint64

func genMessageID(from string) string {
	domain := "localhost"
	if i := strings.LastIndex(from, "@"); i != -1 && i+1 < len(from) {
		domain = from[i+1:]
	}
	n := atomic.AddUint64(&idCounter, 1)
	return fmt.Sprintf("%d.%d@%s", time.Now().UnixNano(), n, domain)
}
