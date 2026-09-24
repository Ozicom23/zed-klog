package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/textproto"
	"strconv"
	"sync"
)

// message is an incoming JSON-RPC message: a request, a notification, or a
// response to a request that the server sent.
type message struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *responseError  `json:"error,omitempty"`
}

func (m *message) isNotification() bool { return m.ID == nil }

type responseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	codeParseError           = -32700
	codeInvalidRequest       = -32600
	codeMethodNotFound       = -32601
	codeInvalidParams        = -32602
	codeServerNotInitialized = -32002
)

// invalidMessageError is a message whose body is not valid JSON. Unlike a
// broken header, it doesn't affect the messages after it.
type invalidMessageError struct{ err error }

func (e *invalidMessageError) Error() string { return "invalid message: " + e.err.Error() }

// connection implements the LSP base protocol: JSON-RPC messages with a
// Content-Length header.
type connection struct {
	reader *bufio.Reader

	mu     sync.Mutex // Guards writer, which the refresh loop also writes to.
	writer io.Writer
}

func newConnection(r io.Reader, w io.Writer) *connection {
	return &connection{reader: bufio.NewReader(r), writer: w}
}

func (c *connection) read() (*message, error) {
	header, err := textproto.NewReader(c.reader).ReadMIMEHeader()
	if err != nil {
		return nil, err
	}
	length, err := strconv.Atoi(header.Get("Content-Length"))
	if err != nil || length < 0 {
		return nil, fmt.Errorf("invalid Content-Length header %q", header.Get("Content-Length"))
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(c.reader, body); err != nil {
		return nil, err
	}
	var m message
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, &invalidMessageError{err}
	}
	return &m, nil
}

func (c *connection) write(v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := fmt.Fprintf(c.writer, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = c.writer.Write(body)
	return err
}

func (c *connection) reply(id json.RawMessage, result any) error {
	return c.write(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  any             `json:"result"`
	}{"2.0", id, result})
}

func (c *connection) replyError(id json.RawMessage, code int, msg string) error {
	return c.write(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   responseError   `json:"error"`
	}{"2.0", id, responseError{code, msg}})
}

func (c *connection) notify(method string, params any) error {
	return c.write(struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params"`
	}{"2.0", method, params})
}

func (c *connection) request(id string, method string) error {
	return c.write(struct {
		JSONRPC string `json:"jsonrpc"`
		ID      string `json:"id"`
		Method  string `json:"method"`
	}{"2.0", id, method})
}
