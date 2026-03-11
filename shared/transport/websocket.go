package transport

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	wsGUID       = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	maxFrameSize = 65535
	WSPath       = "/ws"
)

// WSConn wraps net.Conn with WebSocket binary framing (RFC 6455).
// Client→Server frames carry a 4-byte mask (required by spec).
// Server→Client frames are unmasked.
type WSConn struct {
	conn     net.Conn
	reader   *bufio.Reader
	isClient bool
	rmu      sync.Mutex
	pending  []byte
}

// ClientUpgrade performs the WebSocket handshake from the client side
// over an already-established (TLS) conn.
// host is placed in the Host header and should match the SNI.
func ClientUpgrade(conn net.Conn, host string) (*WSConn, error) {
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("ws key: %w", err)
	}
	keyB64 := base64.StdEncoding.EncodeToString(key)

	req := "GET " + WSPath + " HTTP/1.1\r\n" +
		"Host: " + host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + keyB64 + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"\r\n"

	if _, err := conn.Write([]byte(req)); err != nil {
		return nil, fmt.Errorf("ws upgrade request: %w", err)
	}

	br := bufio.NewReader(conn)

	// Read status line
	statusLine, err := br.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("ws upgrade response: %w", err)
	}
	if !strings.HasPrefix(statusLine, "HTTP/1.1 101") {
		return nil, fmt.Errorf("ws upgrade: expected 101, got %q", strings.TrimSpace(statusLine))
	}

	// Drain response headers
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("ws upgrade headers: %w", err)
		}
		if line == "\r\n" {
			break
		}
	}

	return &WSConn{conn: conn, reader: br, isClient: true}, nil
}

// ServerUpgrade reads the HTTP request and performs the WebSocket handshake.
// Returns (wsConn, nil) on a valid WebSocket upgrade.
// Returns (nil, nil) if not a WebSocket request — a fake HTML page is served and conn is closed.
// Returns (nil, err) on I/O errors.
func ServerUpgrade(conn net.Conn) (*WSConn, error) {
	br := bufio.NewReader(conn)

	// Read request line
	if _, err := br.ReadString('\n'); err != nil {
		return nil, fmt.Errorf("ws server read: %w", err)
	}

	isUpgrade := false
	wsKey := ""

	// Read headers until blank line
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("ws server headers: %w", err)
		}
		if line == "\r\n" {
			break
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "upgrade:") && strings.Contains(lower, "websocket") {
			isUpgrade = true
		}
		if strings.HasPrefix(lower, "sec-websocket-key:") {
			wsKey = strings.TrimSpace(line[len("sec-websocket-key:"):])
		}
	}

	if !isUpgrade {
		serveFakePage(conn)
		return nil, nil
	}

	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + wsAccept(wsKey) + "\r\n" +
		"\r\n"

	if _, err := conn.Write([]byte(resp)); err != nil {
		return nil, fmt.Errorf("ws server response: %w", err)
	}

	return &WSConn{conn: conn, reader: br, isClient: false}, nil
}

// Write sends p as one or more WebSocket binary frames.
func (w *WSConn) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		chunk := p
		if len(chunk) > maxFrameSize {
			chunk = p[:maxFrameSize]
		}
		frame := buildFrame(chunk, w.isClient)
		if _, err := w.conn.Write(frame); err != nil {
			return total, err
		}
		total += len(chunk)
		p = p[len(chunk):]
	}
	return total, nil
}

// Read fills p with payload bytes from incoming WebSocket frames.
func (w *WSConn) Read(p []byte) (int, error) {
	w.rmu.Lock()
	defer w.rmu.Unlock()

	for len(w.pending) == 0 {
		payload, err := readFrame(w.reader)
		if err != nil {
			return 0, err
		}
		if len(payload) > 0 {
			w.pending = payload
		}
	}

	n := copy(p, w.pending)
	w.pending = w.pending[n:]
	return n, nil
}

func (w *WSConn) Close() error                       { return w.conn.Close() }
func (w *WSConn) LocalAddr() net.Addr                { return w.conn.LocalAddr() }
func (w *WSConn) RemoteAddr() net.Addr               { return w.conn.RemoteAddr() }
func (w *WSConn) SetDeadline(t time.Time) error      { return w.conn.SetDeadline(t) }
func (w *WSConn) SetReadDeadline(t time.Time) error  { return w.conn.SetReadDeadline(t) }
func (w *WSConn) SetWriteDeadline(t time.Time) error { return w.conn.SetWriteDeadline(t) }

// ── Frame builder ─────────────────────────────────────────────────────────────

func buildFrame(payload []byte, mask bool) []byte {
	length := len(payload)
	var buf []byte

	buf = append(buf, 0x82) // FIN=1, opcode=2 (binary)

	maskBit := byte(0)
	if mask {
		maskBit = 0x80
	}

	if length < 126 {
		buf = append(buf, maskBit|byte(length))
	} else {
		ext := make([]byte, 2)
		binary.BigEndian.PutUint16(ext, uint16(length))
		buf = append(buf, maskBit|126)
		buf = append(buf, ext...)
	}

	if mask {
		var key [4]byte
		rand.Read(key[:]) //nolint:errcheck
		buf = append(buf, key[:]...)
		for i, b := range payload {
			buf = append(buf, b^key[i%4])
		}
	} else {
		buf = append(buf, payload...)
	}

	return buf
}

func readFrame(r io.Reader) ([]byte, error) {
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return nil, err
	}

	masked := (hdr[1] & 0x80) != 0
	payLen := uint64(hdr[1] & 0x7f)

	switch payLen {
	case 126:
		ext := make([]byte, 2)
		if _, err := io.ReadFull(r, ext); err != nil {
			return nil, err
		}
		payLen = uint64(binary.BigEndian.Uint16(ext))
	case 127:
		ext := make([]byte, 8)
		if _, err := io.ReadFull(r, ext); err != nil {
			return nil, err
		}
		payLen = binary.BigEndian.Uint64(ext)
	}

	var maskKey [4]byte
	if masked {
		if _, err := io.ReadFull(r, maskKey[:]); err != nil {
			return nil, err
		}
	}

	payload := make([]byte, payLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}

	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}

	return payload, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func wsAccept(key string) string {
	h := sha1.New()
	h.Write([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func serveFakePage(conn net.Conn) {
	body := `<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><title>Welcome</title></head>
<body>
<h1>It works!</h1>
<p>This is the default web page for this server.</p>
<p>The web server software is running but no content has been added yet.</p>
</body>
</html>`
	fmt.Fprintf(conn,
		"HTTP/1.1 200 OK\r\nContent-Type: text/html; charset=utf-8\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",
		len(body), body,
	)
	conn.Close()
}
