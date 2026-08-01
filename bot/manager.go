package bot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Manager struct {
	mu    sync.RWMutex
	bots  map[string]*Bot
	lobby string
}

func (m *Manager) SetLobby(addr string) {
	m.mu.Lock()
	m.lobby = addr
	m.mu.Unlock()
}

func (m *Manager) Lobby() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lobby
}

func NewManager() *Manager {

	return &Manager{bots: make(map[string]*Bot)}
}

func (m *Manager) Load(name, host string, logFn func(string, string)) error {
	m.mu.Lock()
	existing, exists  :=  m.bots[name]
	if exists  &&  existing.IsConnected() {
		m.mu.Unlock()
		return fmt.Errorf("bot %q already loaded and connected", name)
	}
	b := New(name, host)
	b.OnLog = logFn
	b.LobbyFn = m.Lobby
	m.bots[name] = b
	m.mu.Unlock()

	tokenSrc, err := LoadOrAuth(name, LogWriter)
	if err != nil {
		m.mu.Lock()
		delete(m.bots, name)
		m.mu.Unlock()
		return fmt.Errorf("auth: %w", err)
	}

	if err := b.Connect(context.Background(), tokenSrc); err != nil {
		m.mu.Lock()
		delete(m.bots, name)
		m.mu.Unlock()
		return err
	}
	return nil
}




func (m *Manager) Disconnect(name string) error {
	m.mu.RLock()
	b, ok := m.bots[name]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("bot %q not found", name)
	}
	b.Disconnect()
	return nil
}

func (m *Manager) DisconnectAll() {
	m.mu.RLock()
	bots := make([]*Bot, 0, len(m.bots))
	for _, b := range m.bots {
		// May not work everytime idk
		bots = append(bots, b)
	}
	m.mu.RUnlock()
	for _, b := range bots {

		b.Disconnect()
	}

}

func (m *Manager) ListAvailable() []string {
	entries, err := os.ReadDir("tokens")
	if err != nil {
		return nil

	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), ".json"))

	}
	return names
}

func (m *Manager) ReconnectAll(host string, logFn func(string, string)) {
	for _, b := range m.All() {
		bb := b
		go func() {
			bb.Disconnect()
			bb.SetHost(host)
			tokenSrc, err := LoadOrAuth(bb.Name, LogWriter)
			if err != nil {
				if logFn != nil {
					logFn(bb.Name, "reconnect auth: "+err.Error())
				}
				return

				
			}
			if err := bb.Connect(context.Background(), tokenSrc); err != nil {
				if logFn != nil {
					logFn(bb.Name, "reconnect: "+err.Error())
				}
			}
		}()
	}
}

func (m *Manager) Get(name string) (*Bot, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.bots[name]
	return b, ok
}

func (m *Manager) FirstConnectedServer() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, b := range m.bots {
		if b.IsConnected() {
			return b.CurrentServer()
		}
	}
	return ""
}

func (m *Manager) All() []*Bot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := make([]*Bot, 0, len(m.bots))
	for _, b := range m.bots {
		list = append(list, b)
	}
	return list
}
