package deploy

import "sync"

// Store is the in-memory registry of deployments this service instance has
// handled. The control-plane is the system of record — this map only backs
// the GET /v1/deploy/{id} debug endpoint and the stop endpoint's
// container-name lookup. Lost on restart by design.
type Store struct {
	mu   sync.RWMutex
	byID map[string]*Record
}

// Record pairs the wire-format Result with the container name the stop
// endpoint needs.
type Record struct {
	Result        Result
	ContainerName string
}

// NewStore returns an empty registry.
func NewStore() *Store {
	return &Store{byID: make(map[string]*Record)}
}

// Put inserts or replaces the record for a deployment id.
func (s *Store) Put(id string, rec Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[id] = &rec
}

// Get returns a copy of the record, and whether it exists.
func (s *Store) Get(id string) (Record, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.byID[id]
	if !ok {
		return Record{}, false
	}
	return *rec, true
}

// SetStatus updates only the status of a tracked deployment (used by the
// stop endpoint). No-op when the id is unknown.
func (s *Store) SetStatus(id, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec, ok := s.byID[id]; ok {
		rec.Result.Status = status
	}
}
