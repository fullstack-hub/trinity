package lsp

import "encoding/json"

// JSON-RPC 2.0 types

type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonrpcNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *int             `json:"id,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *jsonrpcError    `json:"error,omitempty"`
	Method  string           `json:"method,omitempty"` // for server-initiated requests
	Params  json.RawMessage  `json:"params,omitempty"` // for notifications
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// LSP Initialize types

type InitializeParams struct {
	ProcessID  int          `json:"processId"`
	RootURI    string       `json:"rootUri"`
	Capabilities ClientCaps `json:"capabilities"`
}

type ClientCaps struct {
	TextDocument *TextDocumentClientCaps `json:"textDocument,omitempty"`
}

type TextDocumentClientCaps struct {
	Hover      *HoverCaps      `json:"hover,omitempty"`
	Definition *DefinitionCaps `json:"definition,omitempty"`
	References *ReferencesCaps `json:"references,omitempty"`
}

type HoverCaps struct {
	ContentFormat []string `json:"contentFormat,omitempty"`
}

type DefinitionCaps struct{}
type ReferencesCaps struct{}

type InitializeResult struct {
	Capabilities ServerCaps `json:"capabilities"`
}

type ServerCaps struct {
	HoverProvider            bool `json:"hoverProvider,omitempty"`
	DefinitionProvider       bool `json:"definitionProvider,omitempty"`
	ReferencesProvider       bool `json:"referencesProvider,omitempty"`
	CompletionProvider       any  `json:"completionProvider,omitempty"`
	DocumentSymbolProvider   bool `json:"documentSymbolProvider,omitempty"`
	DiagnosticProvider       any  `json:"diagnosticProvider,omitempty"`
	TextDocumentSync         any  `json:"textDocumentSync,omitempty"`
}

// LSP Document types

type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// Hover

type HoverResult struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// References

type ReferenceParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	Context      ReferenceContext        `json:"context"`
}

type ReferenceContext struct {
	IncludeDeclaration bool `json:"includeDeclaration"`
}

// DocumentSymbol

type DocumentSymbolParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type DocumentSymbol struct {
	Name           string           `json:"name"`
	Kind           int              `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

type SymbolInformation struct {
	Name     string   `json:"name"`
	Kind     int      `json:"kind"`
	Location Location `json:"location"`
}

// Diagnostics (published by server)

type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"` // 1=Error, 2=Warning, 3=Info, 4=Hint
	Message  string `json:"message"`
	Source   string `json:"source,omitempty"`
}
