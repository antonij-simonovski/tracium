package mocks

import (
	"context"
	"sync"
	"time"

	"github.com/tracium/api/internal/oauth"
)

// OAuthStore is an in-memory oauth.Store.
type OAuthStore struct {
	mu      sync.Mutex
	clients map[string]oauth.Client
	codes   map[string]oauthCode
}

type oauthCode struct {
	code    oauth.Code
	expires time.Time
}

func NewOAuthStore() *OAuthStore {
	return &OAuthStore{clients: map[string]oauth.Client{}, codes: map[string]oauthCode{}}
}

func (s *OAuthStore) InsertClient(_ context.Context, c oauth.Client) (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[c.ID] = c
	return time.Now(), nil
}

func (s *OAuthStore) Client(_ context.Context, id string) (*oauth.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.clients[id]
	if !ok {
		return nil, oauth.ErrNotFound
	}
	return &c, nil
}

func (s *OAuthStore) InsertCode(_ context.Context, hash string, c oauth.Code, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.codes[hash] = oauthCode{c, time.Now().Add(ttl)}
	return nil
}

func (s *OAuthStore) ConsumeCode(_ context.Context, hash string) (*oauth.Code, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.codes[hash]
	delete(s.codes, hash)
	if !ok || time.Now().After(c.expires) {
		return nil, oauth.ErrNotFound
	}
	return &c.code, nil
}
