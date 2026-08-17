package profile

import "sync"

// Store is an in-memory profile repository, keyed by user id.
//
// plan.md designs this as a Firestore-backed repository; this
// implementation stands in for that until real GCP credentials are
// available to test against. Get/Set's shape matches what a
// Firestore-backed Store would expose, so swapping the backing store later
// only touches this file.
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
