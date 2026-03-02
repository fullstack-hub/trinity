package lsp

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ServerConfig describes how to launch a specific LSP server.
type ServerConfig struct {
	Name       string   // display name (e.g. "gopls", "sourcekit-lsp")
	Command    string   // binary to run
	Args       []string // command-line arguments
	Extensions []string // file extensions that trigger this LSP
	Languages  []string // LSP language identifiers
	NpmPkg     string   // npm package name for auto-install (empty if not npm-based)
}

// knownServers lists all supported LSP servers.
var knownServers = []ServerConfig{
	{Name: "gopls", Command: "gopls", Args: []string{"serve"}, Extensions: []string{".go"}, Languages: []string{"go"}},
	{Name: "typescript-ls", Command: "typescript-language-server", Args: []string{"--stdio"}, Extensions: []string{".ts", ".tsx", ".js", ".jsx"}, Languages: []string{"typescript", "typescriptreact", "javascript", "javascriptreact"}, NpmPkg: "typescript-language-server"},
	{Name: "sourcekit-lsp", Command: "sourcekit-lsp", Args: nil, Extensions: []string{".swift"}, Languages: []string{"swift"}},
	{Name: "kotlin-ls", Command: "kotlin-language-server", Args: nil, Extensions: []string{".kt", ".kts"}, Languages: []string{"kotlin"}},
	{Name: "rust-analyzer", Command: "rust-analyzer", Args: nil, Extensions: []string{".rs"}, Languages: []string{"rust"}},
	{Name: "pyright", Command: "pyright-langserver", Args: []string{"--stdio"}, Extensions: []string{".py"}, Languages: []string{"python"}, NpmPkg: "pyright"},
	{Name: "clangd", Command: "clangd", Args: nil, Extensions: []string{".c", ".cpp", ".h", ".hpp", ".cc", ".cxx", ".m", ".mm"}, Languages: []string{"c", "cpp", "objective-c", "objective-cpp"}},
	{Name: "bash-ls", Command: "bash-language-server", Args: []string{"start"}, Extensions: []string{".sh", ".bash", ".zsh"}, Languages: []string{"shellscript"}, NpmPkg: "bash-language-server"},
	{Name: "yaml-ls", Command: "yaml-language-server", Args: []string{"--stdio"}, Extensions: []string{".yaml", ".yml"}, Languages: []string{"yaml"}, NpmPkg: "yaml-language-server"},
	{Name: "lua-ls", Command: "lua-language-server", Args: nil, Extensions: []string{".lua"}, Languages: []string{"lua"}},
	{Name: "json-ls", Command: "vscode-json-language-server", Args: []string{"--stdio"}, Extensions: []string{".json", ".jsonc"}, Languages: []string{"json", "jsonc"}, NpmPkg: "vscode-langservers-extracted"},
	{Name: "css-ls", Command: "vscode-css-language-server", Args: []string{"--stdio"}, Extensions: []string{".css", ".scss", ".less"}, Languages: []string{"css", "scss", "less"}, NpmPkg: "vscode-langservers-extracted"},
	{Name: "html-ls", Command: "vscode-html-language-server", Args: []string{"--stdio"}, Extensions: []string{".html", ".htm"}, Languages: []string{"html"}, NpmPkg: "vscode-langservers-extracted"},
}

// DetectServers scans the workspace for files and returns LSP configs
// for servers whose binary is available. Installs npm-based servers if needed.
func DetectServers(workspace string) []ServerConfig {
	needed := make(map[string]bool)

	// Walk up to 3 levels deep, skip hidden dirs and common junk
	filepath.Walk(workspace, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		name := info.Name()

		if info.IsDir() {
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" ||
				name == "build" || name == "dist" || name == "__pycache__" || name == "target" ||
				name == "Pods" || name == "DerivedData" {
				return filepath.SkipDir
			}
			rel, _ := filepath.Rel(workspace, path)
			if strings.Count(rel, string(filepath.Separator)) >= 3 {
				return filepath.SkipDir
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(name))
		if ext != "" {
			needed[ext] = true
		}
		return nil
	})

	// Also check parent directories for project files (monorepo support)
	parentExts := detectParentExts(workspace, 2)
	for ext := range parentExts {
		needed[ext] = true
	}

	// Match extensions to server configs, find or install binaries
	var result []ServerConfig
	seen := make(map[string]bool)
	var toInstall []ServerConfig

	for _, cfg := range knownServers {
		if seen[cfg.Name] {
			continue
		}
		for _, ext := range cfg.Extensions {
			if needed[ext] {
				if binPath, ok := findBinary(cfg.Command); ok {
					resolved := cfg
					resolved.Command = binPath
					result = append(result, resolved)
					seen[cfg.Name] = true
				} else if cfg.NpmPkg != "" {
					toInstall = append(toInstall, cfg)
				}
				break
			}
		}
	}

	// Auto-install missing npm-based servers
	for _, cfg := range toInstall {
		if seen[cfg.Name] {
			continue
		}
		if installNpmServer(cfg.NpmPkg) {
			if binPath, ok := findBinary(cfg.Command); ok {
				resolved := cfg
				resolved.Command = binPath
				result = append(result, resolved)
				seen[cfg.Name] = true
			}
		}
	}

	return result
}

// findBinary searches for a binary in PATH and common additional locations.
func findBinary(name string) (string, bool) {
	// Standard PATH lookup
	if path, err := exec.LookPath(name); err == nil {
		return path, true
	}

	home, _ := os.UserHomeDir()
	if home == "" {
		return "", false
	}

	// Search nvm node versions (newest first)
	nvmDir := filepath.Join(home, ".nvm/versions/node")
	if entries, err := os.ReadDir(nvmDir); err == nil {
		for i := len(entries) - 1; i >= 0; i-- {
			if entries[i].IsDir() {
				binPath := filepath.Join(nvmDir, entries[i].Name(), "bin", name)
				if _, err := os.Stat(binPath); err == nil {
					return binPath, true
				}
			}
		}
	}

	// Common additional paths
	additional := []string{
		"/opt/homebrew/bin",
		"/usr/local/bin",
		filepath.Join(home, ".local/bin"),
		filepath.Join(home, "go/bin"),
		filepath.Join(home, ".cargo/bin"),
	}

	for _, dir := range additional {
		binPath := filepath.Join(dir, name)
		if _, err := os.Stat(binPath); err == nil {
			return binPath, true
		}
	}

	return "", false
}

// installNpmServer attempts to install an npm package globally.
func installNpmServer(pkg string) bool {
	// Find npm binary
	npmPath, ok := findBinary("npm")
	if !ok {
		return false
	}
	cmd := exec.Command(npmPath, "install", "-g", pkg)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

// detectParentExts checks parent directories for additional file extensions.
func detectParentExts(workspace string, levels int) map[string]bool {
	exts := make(map[string]bool)
	dir := workspace
	for i := 0; i < levels; i++ {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		entries, err := os.ReadDir(parent)
		if err != nil {
			break
		}
		for _, e := range entries {
			if !e.IsDir() {
				ext := strings.ToLower(filepath.Ext(e.Name()))
				if ext != "" {
					exts[ext] = true
				}
			}
		}
		dir = parent
	}
	return exts
}
