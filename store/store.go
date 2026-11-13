// Package store is a thread-safe in-memory index of synced messages,
// grouped into threads via In-Reply-To/References.
package store

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"mailsvc/message"
)

type Store struct {
	mu       sync.RWMutex
	messages map[string]*message.Message  // key -> message
	byMsgID  map[string]string            // Message-ID -> key
	threads  map[string]map[string]struct{} // threadID -> set of keys
}

func New() *Store {
	return &Store{
		messages: make(map[string]*message.Message),
		byMsgID:  make(map[string]string),
		threads:  make(map[string]map[string]struct{}),
	}
}

func key(account, mailbox string, uid uint32) string {
	return fmt.Sprintf("%s|%s|%d", account, mailbox, uid)
}

// Upsert stores or updates a message and (re)computes its thread membership.
// Safe for concurrent use; called from each account's IMAP sync goroutine.
func (s *Store) Upsert(m *message.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	k := key(m.AccountID, m.Mailbox, m.UID)
	m.ThreadID = s.resolveThreadID(m, k)

	s.messages[k] = m
	if m.MessageID != "" {
		s.byMsgID[m.MessageID] = k
	}
	if s.threads[m.ThreadID] == nil {
		s.threads[m.ThreadID] = make(map[string]struct{})
	}
	s.threads[m.ThreadID][k] = struct{}{}
}

// resolveThreadID looks for an already-known ancestor (via References or
// In-Reply-To) and reuses its thread; otherwise it roots a new thread at the
// oldest reference it knows about, or at the message itself.
func (s *Store) resolveThreadID(m *message.Message, selfKey string) string {
	candidates := append(append([]string{}, m.References...), m.InReplyTo)
	for _, ref := range candidates {
		if ref == "" {
			continue
		}
		if k, ok := s.byMsgID[ref]; ok {
			if existing, ok := s.messages[k]; ok && existing.ThreadID != "" {
				return existing.ThreadID
			}
		}
	}
	if len(m.References) > 0 {
		return "thread:" + m.References[0]
	}
	if m.InReplyTo != "" {
		return "thread:" + m.InReplyTo
	}
	if m.MessageID != "" {
		return "thread:" + m.MessageID
	}
	return "thread:" + selfKey
}

func (s *Store) Get(account, mailbox string, uid uint32) (*message.Message, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.messages[key(account, mailbox, uid)]
	return m, ok
}

func (s *Store) GetByMessageID(msgID string) (*message.Message, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	k, ok := s.byMsgID[msgID]
	if !ok {
		return nil, false
	}
	m, ok := s.messages[k]
	return m, ok
}

// Thread returns every message in a thread, oldest first.
func (s *Store) Thread(threadID string) []*message.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*message.Message, 0, len(s.threads[threadID]))
	for k := range s.threads[threadID] {
		if m, ok := s.messages[k]; ok {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out
}

// ListMailbox returns every message synced for an account's mailbox, oldest first.
func (s *Store) ListMailbox(account, mailbox string) []*message.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	prefix := account + "|" + mailbox + "|"
	var out []*message.Message
	for k, m := range s.messages {
		if strings.HasPrefix(k, prefix) {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out
}
