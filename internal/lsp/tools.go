package lsp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DiagnosticSummary returns a human-readable summary of diagnostics for all open files.
func (m *Manager) DiagnosticSummary() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var sb strings.Builder
	for _, c := range m.clients {
		if c.State != StateRunning {
			continue
		}
		c.diagMu.Lock()
		for uri, diags := range c.diagnostics {
			if len(diags) == 0 {
				continue
			}
			path := uriToPath(uri)
			rel := relativePath(m.workspace, path)
			for _, d := range diags {
				sev := "info"
				switch d.Severity {
				case 1:
					sev = "error"
				case 2:
					sev = "warning"
				}
				sb.WriteString(fmt.Sprintf("%s:%d:%d [%s] %s\n",
					rel, d.Range.Start.Line+1, d.Range.Start.Character+1, sev, d.Message))
			}
		}
		c.diagMu.Unlock()
	}
	if sb.Len() == 0 {
		return ""
	}
	return sb.String()
}

// HoverAt returns hover info for a file at line:col using the appropriate LSP.
func (m *Manager) HoverAt(filePath string, line, col int) (string, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	c := m.ClientForExtension(ext)
	if c == nil {
		return "", fmt.Errorf("no LSP server for %s", ext)
	}
	uri := FileURI(filePath)
	result, err := c.Hover(uri, line, col)
	if err != nil {
		return "", err
	}
	if result == nil {
		return "(no hover info)", nil
	}
	return result.Contents.Value, nil
}

// DefinitionAt returns definition location(s) for a file at line:col.
func (m *Manager) DefinitionAt(filePath string, line, col int) (string, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	c := m.ClientForExtension(ext)
	if c == nil {
		return "", fmt.Errorf("no LSP server for %s", ext)
	}
	uri := FileURI(filePath)
	locs, err := c.Definition(uri, line, col)
	if err != nil {
		return "", err
	}
	if len(locs) == 0 {
		return "(no definition found)", nil
	}
	var sb strings.Builder
	for _, loc := range locs {
		path := uriToPath(loc.URI)
		rel := relativePath(m.workspace, path)
		sb.WriteString(fmt.Sprintf("%s:%d:%d\n", rel, loc.Range.Start.Line+1, loc.Range.Start.Character+1))
	}
	return sb.String(), nil
}

// ReferencesAt returns all references for a symbol at file:line:col.
func (m *Manager) ReferencesAt(filePath string, line, col int) (string, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	c := m.ClientForExtension(ext)
	if c == nil {
		return "", fmt.Errorf("no LSP server for %s", ext)
	}
	uri := FileURI(filePath)
	locs, err := c.References(uri, line, col)
	if err != nil {
		return "", err
	}
	if len(locs) == 0 {
		return "(no references found)", nil
	}
	var sb strings.Builder
	for _, loc := range locs {
		path := uriToPath(loc.URI)
		rel := relativePath(m.workspace, path)
		sb.WriteString(fmt.Sprintf("%s:%d:%d\n", rel, loc.Range.Start.Line+1, loc.Range.Start.Character+1))
	}
	return sb.String(), nil
}

// SymbolsOf returns document symbols for a file.
func (m *Manager) SymbolsOf(filePath string) (string, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	c := m.ClientForExtension(ext)
	if c == nil {
		return "", fmt.Errorf("no LSP server for %s", ext)
	}
	uri := FileURI(filePath)
	raw, err := c.DocumentSymbols(uri)
	if err != nil {
		return "", err
	}

	// Try DocumentSymbol[] first, then SymbolInformation[]
	var docSyms []DocumentSymbol
	if json.Unmarshal(raw, &docSyms) == nil && len(docSyms) > 0 {
		var sb strings.Builder
		for _, s := range docSyms {
			sb.WriteString(fmt.Sprintf("%s %s (L%d)\n", symbolKindName(s.Kind), s.Name, s.Range.Start.Line+1))
			for _, child := range s.Children {
				sb.WriteString(fmt.Sprintf("  %s %s (L%d)\n", symbolKindName(child.Kind), child.Name, child.Range.Start.Line+1))
			}
		}
		return sb.String(), nil
	}

	var symInfos []SymbolInformation
	if json.Unmarshal(raw, &symInfos) == nil && len(symInfos) > 0 {
		var sb strings.Builder
		for _, s := range symInfos {
			sb.WriteString(fmt.Sprintf("%s %s (L%d)\n", symbolKindName(s.Kind), s.Name, s.Location.Range.Start.Line+1))
		}
		return sb.String(), nil
	}

	return "(no symbols found)", nil
}

// OpenFileForLSP reads a file and sends didOpen to the appropriate LSP server.
func (m *Manager) OpenFileForLSP(filePath string) error {
	ext := strings.ToLower(filepath.Ext(filePath))
	c := m.ClientForExtension(ext)
	if c == nil {
		return nil // no LSP for this file type, silently skip
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	langID := extensionToLanguageID(ext)
	return c.DidOpen(FileURI(filePath), langID, string(data))
}

func uriToPath(uri string) string {
	if strings.HasPrefix(uri, "file://") {
		path := strings.TrimPrefix(uri, "file://")
		// Handle percent-encoded characters
		path = strings.ReplaceAll(path, "%20", " ")
		return path
	}
	return uri
}

func relativePath(workspace, path string) string {
	rel, err := filepath.Rel(workspace, path)
	if err != nil {
		return path
	}
	return rel
}

func extensionToLanguageID(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".ts":
		return "typescript"
	case ".tsx":
		return "typescriptreact"
	case ".js":
		return "javascript"
	case ".jsx":
		return "javascriptreact"
	case ".swift":
		return "swift"
	case ".kt", ".kts":
		return "kotlin"
	case ".rs":
		return "rust"
	case ".py":
		return "python"
	case ".c", ".h":
		return "c"
	case ".cpp", ".hpp", ".cc", ".cxx":
		return "cpp"
	case ".m":
		return "objective-c"
	case ".mm":
		return "objective-cpp"
	case ".sh", ".bash", ".zsh":
		return "shellscript"
	case ".yaml", ".yml":
		return "yaml"
	case ".json", ".jsonc":
		return "json"
	case ".lua":
		return "lua"
	default:
		return "plaintext"
	}
}

func symbolKindName(kind int) string {
	names := map[int]string{
		1: "File", 2: "Module", 3: "Namespace", 4: "Package",
		5: "Class", 6: "Method", 7: "Property", 8: "Field",
		9: "Constructor", 10: "Enum", 11: "Interface", 12: "Function",
		13: "Variable", 14: "Constant", 15: "String", 16: "Number",
		17: "Boolean", 18: "Array", 19: "Object", 20: "Key",
		21: "Null", 22: "EnumMember", 23: "Struct", 24: "Event",
		25: "Operator", 26: "TypeParameter",
	}
	if n, ok := names[kind]; ok {
		return n
	}
	return fmt.Sprintf("Kind(%d)", kind)
}
