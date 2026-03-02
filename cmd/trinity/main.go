package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fullstack-hub/trinity/internal/config"
	"github.com/fullstack-hub/trinity/internal/procman"
	"github.com/fullstack-hub/trinity/internal/session"
	"github.com/fullstack-hub/trinity/internal/tui"
	"github.com/fullstack-hub/trinity/internal/version"
)

var banner = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Render(`
 ████████╗██████╗ ██╗███╗   ██╗██╗████████╗██╗   ██╗
 ╚══██╔══╝██╔══██╗██║████╗  ██║██║╚══██╔══╝╚██╗ ██╔╝
    ██║   ██████╔╝██║██╔██╗ ██║██║   ██║    ╚████╔╝
    ██║   ██╔══██╗██║██║╚██╗██║██║   ██║     ╚██╔╝
    ██║   ██║  ██║██║██║ ╚████║██║   ██║      ██║
    ╚═╝   ╚═╝  ╚═╝╚═╝╚═╝  ╚═══╝╚═╝   ╚═╝      ╚═╝
`) + "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("242")).Render(
	"    Unified AI CLI — Claude · Gemini · Copilot") + "\n"

func main() {
	version.Load()

	// Parse flags: -c (continue last session), -s <id> (specific session)
	var (
		continueSession bool
		sessionID       string
	)
	var filteredArgs []string
	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "-c":
			continueSession = true
		case "-s":
			if i+1 < len(os.Args) {
				i++
				sessionID = os.Args[i]
			}
		default:
			filteredArgs = append(filteredArgs, os.Args[i])
		}
	}

	cmd := ""
	if len(filteredArgs) > 0 {
		cmd = filteredArgs[0]
	}

	base := findBase()
	cfg := loadConfig(base)
	pm := procman.New(base)

	switch cmd {
	case "serve":
		fmt.Print(banner)
		fmt.Println("\n  Starting servers...")
		if err := pm.StartAll(cfg.Servers); err != nil {
			fmt.Fprintf(os.Stderr, "  Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("\n  Waiting for health checks...")
		if err := pm.WaitForHealthy(cfg.Servers, 30*time.Second); err != nil {
			fmt.Fprintf(os.Stderr, "  Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("\n  All servers running in background.")
		fmt.Println("  Run 'trinity' to launch TUI.")
		fmt.Println("  Run 'trinity stop' to stop all servers.")

	case "stop":
		fmt.Println("  Stopping servers...")
		pm.StopAll(cfg.Servers)
		fmt.Println("  Done.")

	case "status":
		fmt.Print(banner)
		fmt.Println()
		pm.Status(cfg.Servers)

	case "help", "--help", "-h":
		fmt.Print(banner)
		fmt.Println()
		fmt.Println("  Usage:")
		fmt.Println("    trinity          Launch TUI (new session)")
		fmt.Println("    trinity -c       Continue last session")
		fmt.Println("    trinity -s ID    Continue specific session")
		fmt.Println("    trinity serve    Start all servers in background")
		fmt.Println("    trinity stop     Stop all servers")
		fmt.Println("    trinity status   Show server status")
		fmt.Println()

	default:
		// TUI 모드 — 서버 자동 시작
		allHealthy := true
		for _, name := range []string{"claude", "gemini", "copilot"} {
			if !pm.IsRunning(name) {
				allHealthy = false
				break
			}
		}

		if !allHealthy {
			fmt.Print(banner)
			fmt.Println("\n\n  Servers not running. Starting...")
			if err := pm.StartAll(cfg.Servers); err != nil {
				fmt.Fprintf(os.Stderr, "  Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("  Waiting for health checks...")
			if err := pm.WaitForHealthy(cfg.Servers, 30*time.Second); err != nil {
				fmt.Fprintf(os.Stderr, "  Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("  Ready!")
			time.Sleep(500 * time.Millisecond)
		}

		// Session store
		store, err := session.NewStore()
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Error: %v\n", err)
			os.Exit(1)
		}

		workspace, _ := os.Getwd()

		var sess *session.Session
		if sessionID != "" {
			sess, err = store.Load(sessionID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Session not found: %v\n", err)
				os.Exit(1)
			}
		} else if continueSession {
			sess, _ = store.Latest(workspace) // nil if no sessions exist
		}

		p := tea.NewProgram(tui.NewApp(cfg, store, sess, workspace), tea.WithAltScreen(), tea.WithMouseCellMotion())
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
}

func findBase() string {
	// 1. TRINITY_HOME 환경변수
	if home := os.Getenv("TRINITY_HOME"); home != "" {
		if _, err := os.Stat(filepath.Join(home, "config.yaml")); err == nil {
			return home
		}
	}

	// 2. CWD에서 위로 올라가며 config.yaml 탐색
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "config.yaml")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// 3. 바이너리 위치 기준 탐색 (go install로 설치된 경우)
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exeDir := filepath.Dir(resolved)
			if _, err := os.Stat(filepath.Join(exeDir, "config.yaml")); err == nil {
				return exeDir
			}
		}
	}

	// 4. fallback: 현재 디렉토리
	cwd, _ := os.Getwd()
	return cwd
}

func loadConfig(base string) *config.Config {
	cfg, err := config.Load(filepath.Join(base, "config.yaml"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}
	return cfg
}
