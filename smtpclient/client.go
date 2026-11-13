// Package smtpclient provides a single, mutex-guarded SMTP connection per
// account, so sending doesn't pay a fresh TCP+TLS+AUTH handshake on every
// message, while guaranteeing only one send happens on the wire at a time.
package smtpclient

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
	"sync"
)

type Config struct {
	AccountID string
	Host      string
	Port      int
	Username  string
	Password  string
	TLS       bool // true: implicit TLS (465). false: STARTTLS (587/25)
}

// Client holds one live, authenticated SMTP connection for one account.
// A *smtp.Client must not be used concurrently, so every Send is serialized
// behind mu; concurrent callers queue rather than race on the connection.
type Client struct {
	cfg Config

	mu sync.Mutex
	c  *smtp.Client // nil until first use, or after a failed redial
}

func New(cfg Config) *Client {
	return &Client{cfg: cfg}
}

func (cl *Client) dial() (*smtp.Client, error) {
	addr := fmt.Sprintf("%s:%d", cl.cfg.Host, cl.cfg.Port)

	var c *smtp.Client
	if cl.cfg.TLS {
		conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: cl.cfg.Host})
		if err != nil {
			return nil, err
		}
		if c, err = smtp.NewClient(conn, cl.cfg.Host); err != nil {
			conn.Close()
			return nil, err
		}
	} else {
		var err error
		if c, err = smtp.Dial(addr); err != nil {
			return nil, err
		}
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: cl.cfg.Host}); err != nil {
				c.Close()
				return nil, err
			}
		}
	}

	auth := smtp.PlainAuth("", cl.cfg.Username, cl.cfg.Password, cl.cfg.Host)
	if err := c.Auth(auth); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// ensure returns the live connection, verifying it's still alive with NOOP,
// or dials a fresh one. Caller must hold mu.
func (cl *Client) ensure() (*smtp.Client, error) {
	if cl.c != nil {
		if cl.c.Noop() == nil {
			return cl.c, nil
		}
		cl.c.Close()
		cl.c = nil
	}
	c, err := cl.dial()
	if err != nil {
		return nil, err
	}
	cl.c = c
	return c, nil
}

// Send transmits a raw RFC822 message. On failure it transparently redials
// once, since idle SMTP connections are commonly dropped server-side.
// Only one Send runs at a time per Client — others block on mu until it
// completes.
func (cl *Client) Send(from string, to []string, raw []byte) error {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	c, err := cl.ensure()
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	if err := sendOn(c, from, to, raw); err != nil {
		c.Close()
		cl.c = nil

		c2, dialErr := cl.dial()
		if dialErr != nil {
			return fmt.Errorf("send failed (%v) and redial failed: %w", err, dialErr)
		}
		if err2 := sendOn(c2, from, to, raw); err2 != nil {
			c2.Close()
			return fmt.Errorf("send failed after retry: %w", err2)
		}
		cl.c = c2
		return nil
	}
	return nil
}

func sendOn(c *smtp.Client, from string, to []string, raw []byte) error {
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, addr := range to {
		if err := c.Rcpt(addr); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(raw); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

// Close closes the underlying connection, if one is open.
func (cl *Client) Close() error {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	if cl.c == nil {
		return nil
	}
	err := cl.c.Close()
	cl.c = nil
	return err
}
