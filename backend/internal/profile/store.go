package profile

import "sync"








type Store struct {
	mu   sync.RWMutex
	data map[string]Profile
}

func NewStore() *Store {
	return &Store{data: map[string]Profile{}}
}

func (s *Store) Get(userID string) (Profile, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.data[userID]
	return p, ok
}

func (s *Store) Set(userID string, p Profile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[userID] = p
}
