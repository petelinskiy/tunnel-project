module github.com/yourusername/tunnel-project/client

go 1.21

require (
	github.com/yourusername/tunnel-project/shared v0.0.0
	github.com/refraction-networking/utls v1.6.0
	github.com/armon/go-socks5 v0.0.0-20160902184237-e75332964ef5
	github.com/gorilla/websocket v1.5.1
	github.com/gorilla/mux v1.8.1
	github.com/xtaci/smux v1.5.24
	golang.org/x/crypto v0.18.0
	golang.org/x/net v0.20.0
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.28.0
)

replace github.com/yourusername/tunnel-project/shared => ../shared
