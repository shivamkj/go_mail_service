package mailsvc

// Account groups the IMAP and SMTP settings for one mailbox. Works with any
// standards-compliant provider (Gmail, Outlook/Office365, Fastmail, generic
// hosting) given the right host/port/TLS combination — most providers want
// an app password rather than the account password for this kind of access.
type Account struct {
	ID   string
	IMAP IMAPConfig
	SMTP SMTPConfig
}

type IMAPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	Mailbox  string // defaults to "INBOX" if empty
	TLS      bool   // true: implicit TLS (port 993). false: STARTTLS (port 143)
}

type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	TLS      bool // true: implicit TLS (port 465). false: STARTTLS (port 587)
}
