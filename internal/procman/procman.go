package procman

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fullstack-hub/trinity/internal/config"
)

const logDir = ".pids"

type Manager struct {
	base string
}

func New(base string) *Manager {
	os.MkdirAll(filepath.Join(base, logDir), 0755)
	return &Manager{base: base}
}

func (m *Manager) label(name string) string {
	return "com.trinity." + name
}

func (m *Manager) plistPath(name string) string {
	usr, _ := user.Current()
	return filepath.Join(usr.HomeDir, "Library", "LaunchAgents", m.label(name)+".plist")
}

func (m *Manager) logFile(name string) string {
	return filepath.Join(m.base, logDir, name+".log")
}

func (m *Manager) generatePlist(name string, srv config.ServerConfig) error {
	dir := srv.Dir
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(m.base, dir)
	}

	cmdPath, err := exec.LookPath(srv.Cmd)
	if err != nil {
		cmdPath = srv.Cmd
	}
	args := append([]string{cmdPath}, srv.Args...)
	logFile := m.logFile(name)

	envPath := os.Getenv("PATH")
	envHome := os.Getenv("HOME")

	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + escapeXML(m.label(name)) + `</string>
	<key>ProgramArguments</key>
	<array>
`)
	for _, arg := range args {
		sb.WriteString("\t\t<string>" + escapeXML(arg) + "</string>\n")
	}
	sb.WriteString(`	</array>
	<key>WorkingDirectory</key>
	<string>` + escapeXML(dir) + `</string>
	<key>EnvironmentVariables</key>
	<dict>
		<key>PATH</key>
		<string>` + escapeXML(envPath) + `</string>
		<key>HOME</key>
		<string>` + escapeXML(envHome) + `</string>
	</dict>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>StandardOutPath</key>
	<string>` + escapeXML(logFile) + `</string>
	<key>StandardErrorPath</key>
	<string>` + escapeXML(logFile) + `</string>
</dict>
</plist>
`)

	usr, _ := user.Current()
	os.MkdirAll(filepath.Join(usr.HomeDir, "Library", "LaunchAgents"), 0755)
	return os.WriteFile(m.plistPath(name), []byte(sb.String()), 0644)
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
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

		// Unload stale plist if exists
		if _, err := os.Stat(m.plistPath(name)); err == nil {
			exec.Command("launchctl", "unload", m.plistPath(name)).Run()
		}

		if err := m.generatePlist(name, srv); err != nil {
			return fmt.Errorf("failed to generate plist for %s: %w", name, err)
		}

		cmd := exec.Command("launchctl", "load", m.plistPath(name))
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to start %s: %w\n%s", name, err, string(out))
		}

		fmt.Printf("  ✓ %s registered with launchd\n", name)
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
	plist := m.plistPath(name)
	if _, err := os.Stat(plist); os.IsNotExist(err) {
		return
	}

	exec.Command("launchctl", "unload", plist).Run()
	os.Remove(plist)
	fmt.Printf("  ✗ %s unregistered from launchd\n", name)
}

func (m *Manager) IsRunning(name string) bool {
	return m.getPid(name) > 0
}

func (m *Manager) getPid(name string) int {
	out, err := exec.Command("launchctl", "list").Output()
	if err != nil {
		return 0
	}
	label := m.label(name)
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, label) {
			fields := strings.Fields(line)
			if len(fields) >= 3 && fields[0] != "-" {
				pid, _ := strconv.Atoi(fields[0])
				return pid
			}
		}
	}
	return 0
}

func (m *Manager) Status(servers map[string]config.ServerConfig) {
	for _, name := range []string{"claude", "gemini", "copilot"} {
		srv, ok := servers[name]
		if !ok {
			continue
		}
		pid := m.getPid(name)
		running := pid > 0
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
