package tui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func (a *App) lspDiagnostics() tea.Cmd {
	if a.lspManager == nil {
		a.messages = append(a.messages, chatMessage{
			role: roleSystem, content: "[LSP not available]", ts: time.Now(),
		})
		return nil
	}
	diags := a.lspManager.DiagnosticSummary()
	if diags == "" {
		diags = "No diagnostics"
	}
	a.messages = append(a.messages, chatMessage{
		role: roleSystem, content: "[diagnostics]\n" + diags, ts: time.Now(),
	})
	return nil
}

func (a *App) lspSymbols(text string) tea.Cmd {
	parts := strings.Fields(text)
	if len(parts) < 2 {
		a.messages = append(a.messages, chatMessage{
			role: roleSystem, content: "[usage: /symbols <file>]", ts: time.Now(),
		})
		return nil
	}
	filePath := a.resolveFilePath(parts[1])

	if a.lspManager == nil {
		a.messages = append(a.messages, chatMessage{
			role: roleSystem, content: "[LSP not available]", ts: time.Now(),
		})
		return nil
	}

	// Open the file in LSP first
	a.lspManager.OpenFileForLSP(filePath)

	result, err := a.lspManager.SymbolsOf(filePath)
	if err != nil {
		a.messages = append(a.messages, chatMessage{
			role: roleSystem, content: fmt.Sprintf("[symbols error: %v]", err), ts: time.Now(),
		})
		return nil
	}
	a.messages = append(a.messages, chatMessage{
		role: roleSystem, content: fmt.Sprintf("[symbols: %s]\n%s", parts[1], result), ts: time.Now(),
	})
	return nil
}

func (a *App) lspHover(text string) tea.Cmd {
	filePath, line, col, err := a.parseLSPArgs(text, "/hover")
	if err != nil {
		a.messages = append(a.messages, chatMessage{
			role: roleSystem, content: err.Error(), ts: time.Now(),
		})
		return nil
	}

	a.lspManager.OpenFileForLSP(filePath)

	result, err := a.lspManager.HoverAt(filePath, line-1, col-1) // LSP uses 0-based
	if err != nil {
		a.messages = append(a.messages, chatMessage{
			role: roleSystem, content: fmt.Sprintf("[hover error: %v]", err), ts: time.Now(),
		})
		return nil
	}
	a.messages = append(a.messages, chatMessage{
		role: roleSystem, content: "[hover]\n" + result, ts: time.Now(),
	})
	return nil
}

func (a *App) lspDefinition(text string) tea.Cmd {
	filePath, line, col, err := a.parseLSPArgs(text, "/definition")
	if err != nil {
		a.messages = append(a.messages, chatMessage{
			role: roleSystem, content: err.Error(), ts: time.Now(),
		})
		return nil
	}

	a.lspManager.OpenFileForLSP(filePath)

	result, err := a.lspManager.DefinitionAt(filePath, line-1, col-1)
	if err != nil {
		a.messages = append(a.messages, chatMessage{
			role: roleSystem, content: fmt.Sprintf("[definition error: %v]", err), ts: time.Now(),
		})
		return nil
	}
	a.messages = append(a.messages, chatMessage{
		role: roleSystem, content: "[definition]\n" + result, ts: time.Now(),
	})
	return nil
}

func (a *App) lspReferences(text string) tea.Cmd {
	filePath, line, col, err := a.parseLSPArgs(text, "/references")
	if err != nil {
		a.messages = append(a.messages, chatMessage{
			role: roleSystem, content: err.Error(), ts: time.Now(),
		})
		return nil
	}

	a.lspManager.OpenFileForLSP(filePath)

	result, err := a.lspManager.ReferencesAt(filePath, line-1, col-1)
	if err != nil {
		a.messages = append(a.messages, chatMessage{
			role: roleSystem, content: fmt.Sprintf("[references error: %v]", err), ts: time.Now(),
		})
		return nil
	}
	a.messages = append(a.messages, chatMessage{
		role: roleSystem, content: "[references]\n" + result, ts: time.Now(),
	})
	return nil
}

// parseLSPArgs parses "/command <file> <line> <col>" and returns absolute path, line, col.
func (a *App) parseLSPArgs(text, cmd string) (string, int, int, error) {
	if a.lspManager == nil {
		return "", 0, 0, fmt.Errorf("[LSP not available]")
	}
	parts := strings.Fields(text)
	if len(parts) < 4 {
		return "", 0, 0, fmt.Errorf("[usage: %s <file> <line> <col>]", cmd)
	}
	filePath := a.resolveFilePath(parts[1])
	line, err := strconv.Atoi(parts[2])
	if err != nil {
		return "", 0, 0, fmt.Errorf("[invalid line number: %s]", parts[2])
	}
	col, err := strconv.Atoi(parts[3])
	if err != nil {
		return "", 0, 0, fmt.Errorf("[invalid column number: %s]", parts[3])
	}
	return filePath, line, col, nil
}

// resolveFilePath converts a relative path to absolute using workspace.
func (a *App) resolveFilePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(a.workspace, path)
}
