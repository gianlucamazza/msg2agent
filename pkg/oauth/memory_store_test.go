package oauth

import (
	"sync"
)

// memStore is a minimal in-memory oauth.Store for use in tests.
type memStore struct {
	mu      sync.Mutex
	clients map[string]*Client
	codes   map[string]*Code         // key = CodeHash
	rts     map[string]*RefreshToken // key = TokenHash
}

func newMemStore() *memStore {
	return &memStore{
		clients: make(map[string]*Client),
		codes:   make(map[string]*Code),
		rts:     make(map[string]*RefreshToken),
	}
}

func (s *memStore) PutClient(c *Client) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[c.ClientID] = c
	return nil
}

func (s *memStore) GetClient(clientID string) (*Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.clients[clientID]
	if !ok {
		return nil, ErrClientNotFound
	}
	return c, nil
}

func (s *memStore) PutCode(code *Code) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.codes[code.CodeHash] = code
	return nil
}

func (s *memStore) UseCode(codeHash string) (*Code, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.codes[codeHash]
	if !ok {
		return nil, ErrCodeNotFound
	}
	if c.Used {
		return nil, ErrCodeExpiredOrUsed
	}
	c.Used = true
	return c, nil
}

func (s *memStore) PutRefreshToken(rt *RefreshToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rts[rt.TokenHash] = rt
	return nil
}

func (s *memStore) RotateRefreshToken(oldHash string, newRT *RefreshToken) (*RefreshToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.rts[oldHash]
	if !ok {
		return nil, ErrRefreshTokenNotFound
	}
	if old.Revoked {
		return nil, ErrRefreshTokenRevoked
	}
	old.Revoked = true
	s.rts[newRT.TokenHash] = newRT
	return newRT, nil
}

func (s *memStore) RevokeRefreshToken(hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rt, ok := s.rts[hash]; ok {
		rt.Revoked = true
	}
	return nil
}

func (s *memStore) CleanupOAuthExpired() error { return nil }
