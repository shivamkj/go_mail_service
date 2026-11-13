package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"mailsvc"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	svc := mailsvc.New()

	password := os.Getenv("APP_PASS")
	email := "alice@example.com"

	svc.AddAccount(ctx, mailsvc.Account{
		ID: "work",
		IMAP: mailsvc.IMAPConfig{
			Host: "imap.gmail.com", Port: 993,
			Username: email, Password: password,
			Mailbox: "INBOX", TLS: true,
		},
		SMTP: mailsvc.SMTPConfig{
			Host: "smtp.gmail.com", Port: 587,
			Username: email, Password: password,
			From: email, TLS: false, // STARTTLS
		},
	})

	// Add as many accounts as you like — each gets its own IDLE goroutine
	// and its own pooled SMTP sender.
	// svc.AddAccount(ctx, mailsvc.Account{ID: "personal", ...})

	// Example: send a brand-new message.
	// if err := svc.SendNew("work", []string{"sender@example.org"}, nil,
	// 	"Hello from mailsvc", "Hi there! Hope you are doing well, let's connect for hiring", "", nil); err != nil {
	// 	log.Println("send error:", err)
	// }

	// // Example: reply to a synced message once one exists.
	// replyMsg := message.Message{
	// 	AccountID:   "work",
	// 	Mailbox:     "INBOX",
	// 	UID:         0x5d59,
	// 	MessageID:   "1a0d16b56a8.8820b66b56629.7742560551339625972@example.org",
	// 	InReplyTo:   "",
	// 	References:  []string{},
	// 	ThreadID:    "thread:1a0d16b56a8.8820b66b56629.7742560551339625972@example.org",
	// 	From:        "sender@example.org",
	// 	To:          []string{"recipient@example.net"},
	// 	Cc:          []string(nil),
	// 	Subject:     "Checking Again",
	// 	Date:        time.Date(2026, time.September, 24, 8, 47, 47, 0, time.Local),
	// 	TextBody:    "Hope that your SMTP and IMAP Clients are working fine, can you also confirm that reply is also working.\r\n\r\n\r\nThanks and Regards,\r\nAlice Example",
	// 	HTMLBody:    "<!DOCTYPE html PUBLIC \"-//W3C//DTD HTML 4.01 Transitional//EN\"><html><head><meta content=\"text/html;charset=UTF-8\" http-equiv=\"Content-Type\"></head><body ><div dir='auto' style='font-size:10pt;font-family:Verdana,Arial,Helvetica,sans-serif;'><div id=\"message\" dir=\"auto\"><div><font size=\"2\">Hope that your SMTP and IMAP Clients are working fine, can you also confirm that reply is also working.</font></div><div><font size=\"2\"><br></font></div><div><font size=\"2\">Thanks and Regards,</font></div><div><font size=\"2\">Alice Example</font></div><div><font size=\"2\"><br id=\"br3\"></font><br id=\"br3\"><br id=\"br3\"></div></div><div id=\"signature\" dir=\"auto\"><div><br></div></div><div id=\"content\" dir=\"auto\"></div></div><br></body></html>",
	// 	Attachments: []message.Attachment(nil),
	// 	Flags:       []string{"\\Seen"},
	// }
	// svc.Reply("work", &replyMsg, "Thanks, got it!, reply is also working", "", nil)

	log.Println("mailsvc running — press Ctrl+C to stop")
	<-ctx.Done()
	log.Println("shutting down")
}
