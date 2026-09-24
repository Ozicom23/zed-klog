package main

// The subset of the Language Server Protocol that klog-ls uses.
// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"` // In UTF-16 code units.
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

type TextDocumentItem struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
	Text    string `json:"text"`
}

// documentParams are the params of a request about a document.
type documentParams interface {
	documentURI() string
}

type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

func (p *TextDocumentPositionParams) documentURI() string { return p.TextDocument.URI }
func (p *InlayHintParams) documentURI() string            { return p.TextDocument.URI }
func (p *CodeActionParams) documentURI() string           { return p.TextDocument.URI }

type InitializeParams struct {
	Capabilities struct {
		Workspace struct {
			InlayHint struct {
				RefreshSupport bool `json:"refreshSupport"`
			} `json:"inlayHint"`
		} `json:"workspace"`
	} `json:"capabilities"`
}

type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

type DidChangeTextDocumentParams struct {
	TextDocument struct {
		URI     string `json:"uri"`
		Version int    `json:"version"`
	} `json:"textDocument"`
	ContentChanges []struct {
		Text string `json:"text"`
	} `json:"contentChanges"`
}

type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

const (
	SeverityError   = 1
	SeverityWarning = 2
)

type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"`
	Code     string `json:"code,omitempty"`
	Source   string `json:"source"`
	Message  string `json:"message"`
}

type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Version     int          `json:"version"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type InlayHintParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
}

type InlayHint struct {
	Position    Position `json:"position"`
	Label       string   `json:"label"`
	PaddingLeft bool     `json:"paddingLeft"`
}

type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type Hover struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

const (
	CompletionItemKindValue   = 12
	CompletionItemKindKeyword = 14
)

type CompletionItem struct {
	Label      string    `json:"label"`
	Kind       int       `json:"kind,omitempty"`
	Detail     string    `json:"detail,omitempty"`
	SortText   string    `json:"sortText,omitempty"`
	FilterText string    `json:"filterText,omitempty"`
	TextEdit   *TextEdit `json:"textEdit,omitempty"`
}

type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

type CodeActionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
	Context      struct {
		Only []string `json:"only"`
	} `json:"context"`
}

type WorkspaceEdit struct {
	Changes map[string][]TextEdit `json:"changes"`
}

type CodeAction struct {
	Title string         `json:"title"`
	Kind  string         `json:"kind"`
	Edit  *WorkspaceEdit `json:"edit"`
}
