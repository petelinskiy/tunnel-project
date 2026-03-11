package tunnel

import (
	"embed"
	"fmt"
	"mime"
	"net"
	"path"
	"path/filepath"
	"strings"
)

//go:embed static
var staticFS embed.FS

// serveStaticFile writes the embedded static file matching urlPath to conn.
// Called by transport.FallbackHandler for non-WebSocket HTTP requests.
func serveStaticFile(conn net.Conn, method, urlPath string) {
	defer conn.Close()

	// Normalize path and map clean URLs to files.
	cleanPath := path.Clean("/" + strings.TrimPrefix(urlPath, "/"))
	switch cleanPath {
	case "/":
		cleanPath = "/index.html"
	case "/about":
		cleanPath = "/about.html"
	case "/contact":
		cleanPath = "/contact.html"
	}

	filePath := "static" + cleanPath

	data, err := staticFS.ReadFile(filePath)
	if err != nil {
		body := `<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8"><title>404</title>` +
			`<link rel="stylesheet" href="/style.css"></head><body>` +
			`<div style="display:flex;align-items:center;justify-content:center;min-height:100vh;">` +
			`<div style="text-align:center">` +
			`<p style="font-size:6rem;font-family:serif;opacity:0.15;line-height:1">404</p>` +
			`<p style="color:#777;margin-top:1rem">Page not found</p>` +
			`<p style="margin-top:1.5rem"><a href="/" style="color:#c8a96e;text-decoration:none">← Back to gallery</a></p>` +
			`</div></div></body></html>`
		fmt.Fprintf(conn,
			"HTTP/1.1 404 Not Found\r\nContent-Type: text/html; charset=utf-8\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",
			len(body), body)
		return
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	ct := mime.TypeByExtension(ext)
	if ct == "" {
		ct = "application/octet-stream"
	}

	fmt.Fprintf(conn,
		"HTTP/1.1 200 OK\r\nContent-Type: %s\r\nContent-Length: %d\r\nCache-Control: public, max-age=3600\r\nConnection: close\r\n\r\n",
		ct, len(data))
	conn.Write(data) //nolint:errcheck
}
