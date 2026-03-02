package procman

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fullstack-hub/trinity/internal/config"
)

const pidDir = ".pids"

type Manager struct {
	base string
}

func New(base string) *Manager {
	os.MkdirAll(filepath.Join(base, pidDir), 0755)
	return &Manager{base: base}
}

func (m *Manager) pidFile(name string) string {
	return filepath.Join(m.base, pidDir, name+".pid")
}

func (m *Manager) StartAll(servers map[string]config.ServerConfig) error {
	for _, name := range []string{"claude", "gemini", "copilot"} {
		srv, ok := servers[name]
		if !ok || srv.Cmd == "" {
			continue
		}

		if m.IsRunning(name) {
			fmt.Printf("  ● %s already running\n", name)
			continue
		}

		dir := srv.Dir
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(m.base, dir)
		}

		cmd := exec.Command(srv.Cmd, srv.Args...)
		cmd.Dir = dir
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		logFile, _ := os.Create(filepath.Join(m.base, pidDir, name+".log"))
		cmd.Stdout = logFile
		cmd.Stderr = logFile

		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to start %s: %w", name, err)
		}

		os.WriteFile(m.pidFile(name), []byte(strconv.Itoa(cmd.Process.Pid)), 0644)

		// Detach — don't wait for child
		go cmd.Wait()

		fmt.Printf("  ✓ %s started (pid %d)\n", name, cmd.Process.Pid)
	}
	return nil
}

func (m *Manager) StopAll(servers map[string]config.ServerConfig) {
	for _, name := range []string{"claude", "gemini", "copilot"} {
		if _, ok := servers[name]; !ok {
			continue
		}
		m.Stop(name)
	}
}

func (m *Manager) Stop(name string) {
	pid := m.readPid(name)
	if pid == 0 {
		return
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		os.Remove(m.pidFile(name))
		return
	}

	proc.Signal(syscall.SIGTERM)
	// Give it 3 seconds to gracefully shut down
	done := make(chan struct{})
	go func() {
		proc.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		proc.Kill()
	}

	os.Remove(m.pidFile(name))
	fmt.Printf("  ✗ %s stopped\n", name)
}

func (m *Manager) IsRunning(name string) bool {
	pid := m.readPid(name)
	if pid == 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err != nil {
		os.Remove(m.pidFile(name))
		return false
	}
	return true
}

func (m *Manager) Status(servers map[string]config.ServerConfig) {
	for _, name := range []string{"claude", "gemini", "copilot"} {
		srv, ok := servers[name]
		if !ok {
			continue
		}
		pid := m.readPid(name)
		running := m.IsRunning(name)
		healthy := false
		if running {
			resp, err := http.Get(srv.URL + "/health")
			if err == nil && resp.StatusCode == 200 {
				healthy = true
				resp.Body.Close()
			}
			if resp != nil {
				resp.Body.Close()
			}
		}

		if running && healthy {
			fmt.Printf("  ● %s  running (pid %d)  %s  ✓ healthy\n", name, pid, srv.URL)
		} else if running {
			fmt.Printf("  ● %s  running (pid %d)  %s  ✗ not healthy\n", name, pid, srv.URL)
		} else {
			fmt.Printf("  ○ %s  stopped\n", name)
		}
	}
}

func (m *Manager) WaitForHealthy(servers map[string]config.ServerConfig, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for _, name := range []string{"claude", "gemini", "copilot"} {
		srv, ok := servers[name]
		if !ok || srv.Cmd == "" {
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

func (m *Manager) readPid(name string) int {
	data, err := os.ReadFile(m.pidFile(name))
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return pid
}
