module github.com/yourusername/tunnel-project/client

go 1.21

require (
	github.com/armon/go-socks5 v0.0.0-20160902184237-e75332964ef5
	github.com/gorilla/mux v1.8.1
	github.com/gorilla/websocket v1.5.1
	github.com/hashicorp/yamux v0.1.1
	github.com/refraction-networking/utls v1.6.0
	github.com/yourusername/tunnel-project/shared v0.0.0
	golang.org/x/crypto v0.18.0
	golang.org/x/net v0.20.0
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.28.0
)

require (
	github.com/andybalholm/brotli v1.0.5 // indirect
	github.com/cloudflare/circl v1.3.6 // indirect
	github.com/klauspost/compress v1.16.7 // indirect
	github.com/quic-go/quic-go v0.37.4 // indirect
	golang.org/x/sys v0.16.0 // indirect
)

replace github.com/yourusername/tunnel-project/shared => ../shared
