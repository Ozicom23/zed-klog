package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"time"
)

type server struct {
	conn *connection
	now  func() time.Time

	mu        sync.Mutex // Guards documents, which the refresh loop also reads.
	documents map[string]*document

	initialized    bool
	shutdown       bool
	exited         bool
	refreshSupport bool
	stopRefresh    chan struct{}
}

func newServer(r io.Reader, w io.Writer, now func() time.Time) *server {
	return &server{
		conn:        newConnection(r, w),
		now:         now,
		documents:   map[string]*document{},
		stopRefresh: make(chan struct{}),
	}
}

// run processes messages until the client sends `exit` or closes the input.
func (s *server) run() error {
	defer close(s.stopRefresh)
	for !s.exited {
		m, err := s.conn.read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		var invalid *invalidMessageError
		if errors.As(err, &invalid) {
			log.Println(err)
			if err := s.conn.replyError(json.RawMessage("null"), codeParseError, err.Error()); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if err := s.handle(m); err != nil {
			return err
		}
	}
	return nil
}

func (s *server) exitCode() int {
	if s.shutdown {
		return 0
	}
	return 1
}

func (s *server) handle(m *message) error {
	if m.Method == "" {
		return nil // A response to one of our requests.
	}
	if !s.initialized && m.Method != "initialize" && m.Method != "exit" {
		if m.isNotification() {
			return nil
		}
		return s.conn.replyError(m.ID, codeServerNotInitialized, "server not initialized")
	}
	if s.shutdown && m.Method != "exit" {
		if m.isNotification() {
			return nil
		}
		return s.conn.replyError(m.ID, codeInvalidRequest, "server is shutting down")
	}

	switch m.Method {
	case "initialize":
		if s.initialized {
			return s.conn.replyError(m.ID, codeInvalidRequest, "server is already initialized")
		}
		var params InitializeParams
		if err := json.Unmarshal(m.Params, &params); err != nil {
			return s.conn.replyError(m.ID, codeInvalidParams, err.Error())
		}
		s.initialized = true
		s.refreshSupport = params.Capabilities.Workspace.InlayHint.RefreshSupport
		if s.refreshSupport {
			go s.refreshLoop()
		}
		return s.conn.reply(m.ID, initializeResult())
	case "initialized":
		return nil
	case "shutdown":
		s.shutdown = true
		return s.conn.reply(m.ID, nil)
	case "exit":
		s.exited = true
		return nil

	case "textDocument/didOpen":
		var params DidOpenTextDocumentParams
		if err := json.Unmarshal(m.Params, &params); err != nil {
			return nil
		}
		doc := params.TextDocument
		return s.update(parseDocument(doc.URI, doc.Version, doc.Text))
	case "textDocument/didChange":
		var params DidChangeTextDocumentParams
		if err := json.Unmarshal(m.Params, &params); err != nil || len(params.ContentChanges) == 0 {
			return nil
		}
		// The server asks for full document sync, so the last change has the whole text.
		text := params.ContentChanges[len(params.ContentChanges)-1].Text
		return s.update(parseDocument(params.TextDocument.URI, params.TextDocument.Version, text))
	case "textDocument/didClose":
		var params DidCloseTextDocumentParams
		if err := json.Unmarshal(m.Params, &params); err != nil {
			return nil
		}
		s.mu.Lock()
		delete(s.documents, params.TextDocument.URI)
		s.mu.Unlock()
		return s.conn.notify("textDocument/publishDiagnostics",
			PublishDiagnosticsParams{URI: params.TextDocument.URI, Diagnostics: []Diagnostic{}})

	case "textDocument/inlayHint":
		var params InlayHintParams
		return s.respond(m, &params, func(d *document) any {
			return d.inlayHints(params.Range, s.now())
		})
	case "textDocument/hover":
		var params TextDocumentPositionParams
		return s.respond(m, &params, func(d *document) any {
			return d.hover(params.Position, s.now())
		})
	case "textDocument/completion":
		var params TextDocumentPositionParams
		return s.respond(m, &params, func(d *document) any {
			return d.completion(params.Position)
		})
	case "textDocument/codeAction":
		var params CodeActionParams
		return s.respond(m, &params, func(d *document) any {
			return d.codeActions(params.Range.Start.Line, params.Context.Only, s.now())
		})
	}

	if m.isNotification() {
		return nil // E.g. `$/cancelRequest` or `workspace/didChangeConfiguration`.
	}
	return s.conn.replyError(m.ID, codeMethodNotFound, fmt.Sprintf("method not supported: %s", m.Method))
}

func initializeResult() any {
	return map[string]any{
		"capabilities": map[string]any{
			"positionEncoding": "utf-16",
			"textDocumentSync": map[string]any{
				"openClose": true,
				"change":    1, // Full document.
			},
			"hoverProvider":     true,
			"inlayHintProvider": true,
			"completionProvider": map[string]any{
				"triggerCharacters": []string{"#", "="},
			},
			"codeActionProvider": map[string]any{
				"codeActionKinds": []string{codeActionKind},
			},
		},
		"serverInfo": map[string]any{"name": "klog-ls", "version": version},
	}
}

// update stores the document and publishes its diagnostics.
func (s *server) update(d *document) error {
	s.mu.Lock()
	s.documents[d.uri] = d
	s.mu.Unlock()
	return s.conn.notify("textDocument/publishDiagnostics", PublishDiagnosticsParams{
		URI:         d.uri,
		Version:     d.version,
		Diagnostics: d.diagnostics(s.now()),
	})
}

// respond decodes the params of a request about a document, and replies with
// the result of `f` for that document.
func (s *server) respond(m *message, params documentParams, f func(*document) any) error {
	if err := json.Unmarshal(m.Params, params); err != nil {
		return s.conn.replyError(m.ID, codeInvalidParams, err.Error())
	}
	s.mu.Lock()
	d := s.documents[params.documentURI()]
	s.mu.Unlock()
	if d == nil {
		return s.conn.reply(m.ID, nil)
	}
	return s.conn.reply(m.ID, f(d))
}

// refreshLoop asks the client to update inlay hints every minute while an
// open range is running, so that the totals keep up with the time.
func (s *server) refreshLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for n := 1; ; n++ {
		select {
		case <-s.stopRefresh:
			return
		case <-ticker.C:
		}
		if !s.hasRunningOpenRange() {
			continue
		}
		if err := s.conn.request(fmt.Sprintf("refresh-%d", n), "workspace/inlayHint/refresh"); err != nil {
			log.Println("refreshing inlay hints:", err)
		}
	}
}

func (s *server) hasRunningOpenRange() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for _, d := range s.documents {
		for _, r := range d.records {
			if or := r.record.OpenRange(); or != nil {
				if _, ok := runningTime(r.record.Date(), or, now); ok {
					return true
				}
			}
		}
	}
	return false
}
