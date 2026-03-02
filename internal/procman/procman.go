package procman

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/fullstack-hub/trinity/internal/config"
)

type Process struct {
	Name string
	Cmd  *exec.Cmd
}

type Manager struct {
	procs []*Process
	mu    sync.Mutex
	base  string
}

func New(base string) *Manager {
	return &Manager{base: base}
}

func (m *Manager) StartAll(servers map[string]config.ServerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, srv := range servers {
		if srv.Cmd == "" {
			continue
		}

		dir := srv.Dir
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(m.base, dir)
		}

		cmd := exec.Command(srv.Cmd, srv.Args...)
		cmd.Dir = dir

		if err := cmd.Start(); err != nil {
			m.stopAll()
			return fmt.Errorf("failed to start %s: %w", name, err)
		}

		m.procs = append(m.procs, &Process{Name: name, Cmd: cmd})
		fmt.Printf("  ✓ %s started (pid %d)\n", name, cmd.Process.Pid)
	}

	return nil
}

func (m *Manager) WaitForHealthy(servers map[string]config.ServerConfig, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for name, srv := range servers {
		if srv.Cmd == "" {
			continue
		}
		if err := waitForServer(ctx, name, srv.URL+"/health"); err != nil {
			return err
		}
	}
	return nil
}

func waitForServer(ctx context.Context, name, url string) error {
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s did not become healthy in time", name)
		default:
			resp, err := http.Get(url)
			if err == nil && resp.StatusCode == 200 {
				resp.Body.Close()
				fmt.Printf("  ✓ %s healthy\n", name)
				return nil
			}
			if resp != nil {
				resp.Body.Close()
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
}

func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopAll()
}

func (m *Manager) stopAll() {
	for _, p := range m.procs {
		if p.Cmd.Process != nil {
			p.Cmd.Process.Kill()
			p.Cmd.Wait()
			fmt.Printf("  ✗ %s stopped\n", p.Name)
		}
	}
	m.procs = nil
}
