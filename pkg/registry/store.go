package registry

import (
	"encoding/json"
	"errors"
	"os"
	"sync"

	"github.com/google/uuid"
)

// Store errors.
var (
	ErrAgentNotFound      = errors.New("agent not found")
	ErrAgentAlreadyExists = errors.New("agent already exists")
)

// Store is the interface for agent storage.
type Store interface {
	// Get retrieves an agent by ID.
	Get(id uuid.UUID) (*Agent, error)

	// GetByDID retrieves an agent by DID.
	GetByDID(did string) (*Agent, error)

	// Put stores an agent.
	Put(agent *Agent) error

	// Delete removes an agent by ID.
	Delete(id uuid.UUID) error

	// List returns all agents.
	List() ([]*Agent, error)

	// Search searches for agents by capability.
	Search(capability string) ([]*Agent, error)
}

// MemoryStore is an in-memory implementation of Store.
type MemoryStore struct {
	agents map[uuid.UUID]*Agent
	byDID  map[string]uuid.UUID
	mu     sync.RWMutex
}

// NewMemoryStore creates a new in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		agents: make(map[uuid.UUID]*Agent),
		byDID:  make(map[string]uuid.UUID),
	}
}

// Get retrieves an agent by ID.
func (s *MemoryStore) Get(id uuid.UUID) (*Agent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	agent, ok := s.agents[id]
	if !ok {
		return nil, ErrAgentNotFound
	}
	return agent, nil
}

// GetByDID retrieves an agent by DID.
func (s *MemoryStore) GetByDID(did string) (*Agent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	id, ok := s.byDID[did]
	if !ok {
		return nil, ErrAgentNotFound
	}
	return s.agents[id], nil
}

// Put stores an agent.
func (s *MemoryStore) Put(agent *Agent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.agents[agent.ID] = agent
	if agent.DID != "" {
		s.byDID[agent.DID] = agent.ID
	}
	return nil
}

// Delete removes an agent by ID.
func (s *MemoryStore) Delete(id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	agent, ok := s.agents[id]
	if !ok {
		return ErrAgentNotFound
	}

	if agent.DID != "" {
		delete(s.byDID, agent.DID)
	}
	delete(s.agents, id)
	return nil
}

// List returns all agents.
func (s *MemoryStore) List() ([]*Agent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	agents := make([]*Agent, 0, len(s.agents))
	for _, agent := range s.agents {
		agents = append(agents, agent)
	}
	return agents, nil
}

func (s *MemoryStore) Search(capability string) ([]*Agent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Agent
	for _, agent := range s.agents {
		if agent.HasCapability(capability) {
			result = append(result, agent)
		}
	}
	return result, nil
}

// FileStore wraps MemoryStore with file persistence.
type FileStore struct {
	*MemoryStore
	path string
}

// NewFileStore creates a new file-backed store.
func NewFileStore(path string) (*FileStore, error) {
	fs := &FileStore{
		MemoryStore: NewMemoryStore(),
		path:        path,
	}

	// Load existing data if file exists
	if _, err := os.Stat(path); err == nil {
		if err := fs.load(); err != nil {
			return nil, err
		}
	}

	return fs, nil
}

// load reads agents from the file.
func (s *FileStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}

	var agents []*Agent
	if err := json.Unmarshal(data, &agents); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, agent := range agents {
		s.agents[agent.ID] = agent
		if agent.DID != "" {
			s.byDID[agent.DID] = agent.ID
		}
	}

	return nil
}

// save writes agents to the file.
func (s *FileStore) save() error {
	agents, _ := s.List()
	data, err := json.MarshalIndent(agents, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}

func (s *FileStore) Put(agent *Agent) error {
	if err := s.MemoryStore.Put(agent); err != nil {
		return err
	}
	return s.save()
}

func (s *FileStore) Delete(id uuid.UUID) error {
	if err := s.MemoryStore.Delete(id); err != nil {
		return err
	}
	return s.save()
}
