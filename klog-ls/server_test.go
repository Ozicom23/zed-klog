package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

// client talks to a server over the LSP base protocol, like an editor does.
type client struct {
	t    *testing.T
	in   io.WriteCloser
	conn *connection
	done chan int
}

func startServer(t *testing.T) *client {
	serverIn, clientOut := io.Pipe()
	clientIn, serverOut := io.Pipe()
	s := newServer(serverIn, serverOut, func() time.Time { return now })
	c := &client{t: t, in: clientOut, conn: newConnection(clientIn, clientOut), done: make(chan int, 1)}
	go func() {
		if err := s.run(); err != nil {
			t.Errorf("server failed: %v", err)
		}
		c.done <- s.exitCode()
		serverOut.Close()
	}()
	return c
}

func (c *client) send(id int, method string, params any) {
	c.t.Helper()
	m := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
	if id != 0 {
		m["id"] = id
	}
	if err := c.conn.write(m); err != nil {
		c.t.Fatal(err)
	}
}

// receive returns the next message, which must be a response or notification
// with the given ID or method.
func (c *client) receive(want string) map[string]any {
	c.t.Helper()
	m, err := c.conn.read()
	if err != nil {
		c.t.Fatal(err)
	}
	if got := string(m.ID) + m.Method; got != want {
		c.t.Fatalf("got message %q, want %q", got, want)
	}
	var full map[string]any
	raw, _ := json.Marshal(m)
	_ = json.Unmarshal(raw, &full)
	return full
}

// result returns the result of the response to the request with the given ID.
func (c *client) result(id string) string {
	c.t.Helper()
	m, err := c.conn.read()
	if err != nil {
		c.t.Fatal(err)
	}
	if string(m.ID) != id {
		c.t.Fatalf("got message %s%s, want response %s", m.ID, m.Method, id)
	}
	if m.Error != nil {
		c.t.Fatalf("request %s failed: %+v", id, m.Error)
	}
	return string(m.Result)
}

func TestServerSession(t *testing.T) {
	c := startServer(t)
	uri := "file:///work/time.klg"

	c.send(1, "initialize", map[string]any{"capabilities": map[string]any{}})
	var init struct {
		Capabilities map[string]any `json:"capabilities"`
	}
	if err := json.Unmarshal([]byte(c.result("1")), &init); err != nil {
		t.Fatal(err)
	}
	if init.Capabilities["positionEncoding"] != "utf-16" || init.Capabilities["inlayHintProvider"] != true {
		t.Errorf("unexpected capabilities: %+v", init.Capabilities)
	}
	c.send(0, "initialized", map[string]any{})

	c.send(0, "textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "klog", "version": 1,
		"text": "2026-09-24 (8h!)\n    8:00 - 12:00 #work\n    oops\n",
	}})
	diagnostics := c.receive(`textDocument/publishDiagnostics`)
	if !strings.Contains(toJSON(diagnostics), `"code":"ErrorMalformedEntry"`) {
		t.Errorf("expected a diagnostic for the malformed entry: %s", toJSON(diagnostics))
	}

	c.send(0, "textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": "2026-09-24 (8h!)\n    8:00 - 12:00 #work\n"}},
	})
	diagnostics = c.receive(`textDocument/publishDiagnostics`)
	if got := toJSON(diagnostics); !strings.Contains(got, `"diagnostics":[]`) || !strings.Contains(got, `"version":2`) {
		t.Errorf("expected no diagnostics for version 2: %s", got)
	}

	c.send(2, "textDocument/inlayHint", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        map[string]any{"start": map[string]any{"line": 0, "character": 0}, "end": map[string]any{"line": 5, "character": 0}},
	})
	if got := c.result("2"); !strings.Contains(got, `"label":"total 4h, diff -4h"`) {
		t.Errorf("unexpected inlay hints: %s", got)
	}

	c.send(3, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 1, "character": 20},
	})
	if got := c.result("3"); !strings.Contains(got, `**#work**: 4h in 1 entry`) {
		t.Errorf("unexpected hover: %s", got)
	}

	c.send(4, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": "file:///unknown.klg"},
		"position":     map[string]any{"line": 0, "character": 0},
	})
	if got := c.result("4"); got != "null" {
		t.Errorf("unexpected hover for an unknown document: %s", got)
	}

	c.send(5, "textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        map[string]any{"start": map[string]any{"line": 1, "character": 0}, "end": map[string]any{"line": 1, "character": 0}},
		"context":      map[string]any{"diagnostics": []any{}},
	})
	if got := c.result("5"); !strings.Contains(got, `"title":"Start open range at 14:32"`) {
		t.Errorf("unexpected code actions: %s", got)
	}

	c.send(6, "textDocument/definition", map[string]any{})
	if m, err := c.conn.read(); err != nil || m.Error == nil || m.Error.Code != codeMethodNotFound {
		t.Errorf("expected method-not-found, got %+v (%v)", m, err)
	}

	c.send(0, "textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": uri}})
	if got := toJSON(c.receive(`textDocument/publishDiagnostics`)); !strings.Contains(got, `"diagnostics":[]`) {
		t.Errorf("diagnostics were not cleared on close: %s", got)
	}

	c.send(7, "shutdown", nil)
	c.result("7")
	c.send(0, "exit", nil)
	if code := <-c.done; code != 0 {
		t.Errorf("exit code: got %d, want 0", code)
	}
}

func TestServerHandlesProtocolErrors(t *testing.T) {
	c := startServer(t)
	hover := map[string]any{
		"textDocument": map[string]any{"uri": "file:///time.klg"},
		"position":     map[string]any{"line": 0, "character": 0},
	}
	expectError := func(code int) {
		t.Helper()
		m, err := c.conn.read()
		if err != nil || m.Error == nil || m.Error.Code != code {
			t.Fatalf("expected error %d, got %+v (%v)", code, m, err)
		}
	}

	c.send(1, "initialize", map[string]any{"capabilities": map[string]any{}})
	c.result("1")
	c.send(2, "initialize", map[string]any{"capabilities": map[string]any{}})
	expectError(codeInvalidRequest)

	body := `{"jsonrpc": "2.0", "id": 3, "method": `
	if _, err := io.WriteString(c.in, fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)); err != nil {
		t.Fatal(err)
	}
	expectError(codeParseError)

	// The server keeps working after a malformed message.
	c.send(4, "textDocument/hover", hover)
	if got := c.result("4"); got != "null" {
		t.Errorf("unexpected hover: %s", got)
	}

	c.send(5, "shutdown", nil)
	c.result("5")
	c.send(6, "textDocument/hover", hover)
	expectError(codeInvalidRequest)
	c.send(0, "exit", nil)
	if code := <-c.done; code != 0 {
		t.Errorf("exit code: got %d, want 0", code)
	}
}

func toJSON(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}
