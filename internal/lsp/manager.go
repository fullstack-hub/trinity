package lsp

import (
	"sync"
)

// Manager auto-detects and manages multiple LSP server connections.
type Manager struct {
	workspace string
	clients   []*Client
	mu        sync.RWMutex
}

// NewManager creates a new LSP manager for the given workspace.
func NewManager(workspace string) *Manager {
	return &Manager{
		workspace: workspace,
	}
}

// StartAll detects required LSP servers and starts them in parallel.
// Non-blocking: servers start in goroutines and report status via State.
func (m *Manager) StartAll() {
	configs := DetectServers(m.workspace)

	m.mu.Lock()
	for _, cfg := range configs {
		c := NewClient(cfg, m.workspace)
		m.clients = append(m.clients, c)
	}
	m.mu.Unlock()

	// Start each server concurrently
	var wg sync.WaitGroup
	for _, c := range m.clients {
		wg.Add(1)
		go func(client *Client) {
			defer wg.Done()
			client.Start() // errors are captured in client.State/Error
		}(c)
	}

	// Don't block — let them start in background
	go func() {
		wg.Wait()
	}()
}

// StopAll gracefully shuts down all LSP servers.
func (m *Manager) StopAll() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var wg sync.WaitGroup
	for _, c := range m.clients {
		wg.Add(1)
		go func(client *Client) {
			defer wg.Done()
			client.Stop()
		}(c)
	}
	wg.Wait()
}

// ServerInfo is a lightweight summary for display.
type ServerInfo struct {
	Name    string
	State   ClientState
	Error   string
}

// Status returns the current status of all LSP servers.
func (m *Manager) Status() []ServerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	infos := make([]ServerInfo, len(m.clients))
	for i, c := range m.clients {
		infos[i] = ServerInfo{
			Name:  c.Config.Name,
			State: c.State,
			Error: c.Error,
		}
	}
	return infos
}

// Clients returns all LSP clients (for agent tool integration).
func (m *Manager) Clients() []*Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Client, len(m.clients))
	copy(result, m.clients)
	return result
}

// ClientForLanguage returns the first running client that handles the given language ID.
func (m *Manager) ClientForLanguage(langID string) *Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, c := range m.clients {
		if c.State != StateRunning {
			continue
		}
		for _, lang := range c.Config.Languages {
			if lang == langID {
				return c
			}
		}
	}
	return nil
}

// ClientForExtension returns the first running client that handles files with the given extension.
func (m *Manager) ClientForExtension(ext string) *Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, c := range m.clients {
		if c.State != StateRunning {
			continue
		}
		for _, e := range c.Config.Extensions {
			if e == ext {
				return c
			}
		}
	}
	return nil
}

// Count returns the total number of detected LSP servers.
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.clients)
}
