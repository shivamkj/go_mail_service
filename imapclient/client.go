// Package imapclient keeps one IMAP connection per account alive with IDLE
// for realtime sync, reconnecting with backoff on any error.
package imapclient

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"

	"mailsvc/store"
)

type Config struct {
	AccountID string
	Host      string
	Port      int
	Username  string
	Password  string
	Mailbox   string
	TLS       bool // true: implicit TLS (993). false: STARTTLS (143)
}

type Syncer struct {
	cfg   Config
	store *store.Store
}

func NewSyncer(cfg Config, st *store.Store) *Syncer {
	if cfg.Mailbox == "" {
		cfg.Mailbox = "INBOX"
	}
	return &Syncer{cfg: cfg, store: st}
}

// Run connects and keeps the mailbox synced via IDLE until ctx is cancelled,
// reconnecting with exponential backoff whenever the connection drops.
func (s *Syncer) Run(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		if err := s.runOnce(ctx); err != nil {
			log.Printf("[imap:%s] %v — retrying in %s", s.cfg.AccountID, err, backoff)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return
			}
			if backoff < 60*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
	}
}

func (s *Syncer) dial() (*client.Client, error) {
	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)

	var c *client.Client
	var err error
	if s.cfg.TLS {
		c, err = client.DialTLS(addr, nil)
	} else {
		if c, err = client.Dial(addr); err == nil {
			if ok, _ := c.SupportStartTLS(); ok {
				err = c.StartTLS(&tls.Config{ServerName: s.cfg.Host})
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	log.Printf("[imap:%s] connected to %s", s.cfg.AccountID, s.cfg.Host)
	if err := c.Login(s.cfg.Username, s.cfg.Password); err != nil {
		c.Logout()
		return nil, fmt.Errorf("login: %w", err)
	}
	log.Printf("[imap:%s] logged in as %s", s.cfg.AccountID, s.cfg.Username)
	return c, nil
}

// runOnce owns one connection end-to-end: connect, initial sync, then
// IDLE/re-sync in a loop until the connection errors or ctx is cancelled.
func (s *Syncer) runOnce(ctx context.Context) error {
	c, err := s.dial()
	if err != nil {
		return err
	}
	defer c.Logout()

	updates := make(chan client.Update, 16)
	c.Updates = updates

	if _, err := c.Select(s.cfg.Mailbox, false); err != nil {
		return fmt.Errorf("select %s: %w", s.cfg.Mailbox, err)
	}
	log.Printf("[imap:%s] selected mailbox %s", s.cfg.AccountID, s.cfg.Mailbox)

	lastUID, err := s.fetchNew(c, 0)
	if err != nil {
		return fmt.Errorf("initial sync: %w", err)
	}
	log.Printf("[imap:%s] initial sync complete, high-water UID=%d", s.cfg.AccountID, lastUID)

	for {
		stop := make(chan struct{})
		idleErr := make(chan error, 1)
		go func() { idleErr <- c.Idle(stop, nil) }()

		refetch := false
		select {
		case <-ctx.Done():
			close(stop)
			<-idleErr
			return nil
		case upd := <-updates:
			if _, ok := upd.(*client.MailboxUpdate); ok {
				log.Printf("[imap:%s] mailbox update received", s.cfg.AccountID)
				refetch = true
			}
			close(stop)
			if err := <-idleErr; err != nil {
				return fmt.Errorf("idle: %w", err)
			}
		case <-time.After(29 * time.Minute):
			// Refresh before the server's ~30 min IDLE timeout fires.
			log.Printf("[imap:%s] refreshing IDLE connection", s.cfg.AccountID)
			close(stop)
			if err := <-idleErr; err != nil {
				return fmt.Errorf("idle: %w", err)
			}
		}

		if refetch {
			uid, err := s.fetchNew(c, lastUID)
			if err != nil {
				return fmt.Errorf("resync: %w", err)
			}
			log.Printf("[imap:%s] resync complete, high-water UID=%d", s.cfg.AccountID, uid)
			lastUID = uid
		}
	}
}

// fetchNew fetches every message with UID > lastUID (or, on first run, just
// the most recent ~50 so a big mailbox isn't pulled in fully on every
// reconnect) and upserts them into the store. It returns the new high-water
// UID.
func (s *Syncer) fetchNew(c *client.Client, lastUID uint32) (uint32, error) {
	start := lastUID + 1
	if lastUID == 0 {
		start = 1
		if mbox := c.Mailbox(); mbox != nil && mbox.UidNext > 50 {
			start = mbox.UidNext - 50
		}
	}

	criteria := imap.NewSearchCriteria()
	criteria.Uid = new(imap.SeqSet)
	criteria.Uid.AddRange(start, 0) // 0 == "*"

	uids, err := c.UidSearch(criteria)
	if err != nil {
		return lastUID, fmt.Errorf("search: %w", err)
	}
	if len(uids) == 0 {
		return lastUID, nil
	}
	log.Printf("[imap:%s] found %d new message(s) to fetch", s.cfg.AccountID, len(uids))

	seqset := new(imap.SeqSet)
	seqset.AddNum(uids...)

	section := &imap.BodySectionName{}
	items := []imap.FetchItem{imap.FetchUid, imap.FetchFlags, section.FetchItem()}

	messages := make(chan *imap.Message, 16)
	done := make(chan error, 1)
	go func() { done <- c.UidFetch(seqset, items, messages) }()

	maxUID := lastUID
	for msg := range messages {
		if msg.Uid > maxUID {
			maxUID = msg.Uid
		}
		body := msg.GetBody(section)
		if body == nil {
			continue
		}
		m, err := parseMessage(s.cfg.AccountID, s.cfg.Mailbox, msg.Uid, msg.Flags, body)
		if err != nil {
			log.Printf("[imap:%s] parse uid=%d: %v", s.cfg.AccountID, msg.Uid, err)
			continue
		}
		s.store.Upsert(m)
		log.Printf("[imap:%s] uid=%d | from=%s | subject=%s | date=%s", s.cfg.AccountID, m.UID, m.From, m.Subject, m.Date.Format("2006-01-02 15:04"))
	}
	if err := <-done; err != nil {
		return maxUID, fmt.Errorf("fetch: %w", err)
	}
	log.Printf("[imap:%s] fetched messages up to UID=%d", s.cfg.AccountID, maxUID)
	return maxUID, nil
}
