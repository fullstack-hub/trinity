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
	"github.com/fullstack-hub/trinity/internal/tui"
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
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
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
		fmt.Println("    trinity          Launch TUI (servers must be running)")
		fmt.Println("    trinity serve    Start all servers in background")
		fmt.Println("    trinity stop     Stop all servers")
		fmt.Println("    trinity status   Show server status")
		fmt.Println()

	default:
		// TUI 모드
		fmt.Print(banner)
		fmt.Println()

		// 서버 상태 확인
		allHealthy := true
		for _, name := range []string{"claude", "gemini", "copilot"} {
			if !pm.IsRunning(name) {
				allHealthy = false
				break
			}
		}

		if !allHealthy {
			fmt.Println("  Servers not running. Starting...")
			if err := pm.StartAll(cfg.Servers); err != nil {
				fmt.Fprintf(os.Stderr, "  Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("  Waiting for health checks...")
			if err := pm.WaitForHealthy(cfg.Servers, 30*time.Second); err != nil {
				fmt.Fprintf(os.Stderr, "  Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Println()
		}

		pm.Status(cfg.Servers)
		fmt.Println()
		time.Sleep(500 * time.Millisecond)

		p := tea.NewProgram(tui.NewApp(cfg), tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
}

func findBase() string {
	// config.yaml 기준으로 base 디렉토리 찾기
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
	// fallback: 현재 디렉토리
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
