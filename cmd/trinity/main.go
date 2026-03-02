package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fullstack-hub/trinity/internal/config"
	"github.com/fullstack-hub/trinity/internal/procman"
	"github.com/fullstack-hub/trinity/internal/tui"
)

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	base, _ := filepath.Abs(".")

	// 서버 프로세스 시작
	pm := procman.New(base)
	fmt.Println("Starting servers...")
	if err := pm.StartAll(cfg.Servers); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start servers: %v\n", err)
		os.Exit(1)
	}
	defer pm.StopAll()

	// 헬스체크 대기
	fmt.Println("Waiting for servers to be ready...")
	if err := pm.WaitForHealthy(cfg.Servers, 30*time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "Server health check failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("All servers ready. Launching TUI...")
	time.Sleep(500 * time.Millisecond)

	// TUI 시작
	p := tea.NewProgram(tui.NewApp(cfg), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
