package deploy

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"time"

	"golang.org/x/crypto/ssh"
)

const serverBinaryPath = "/app/tunnel-server"

// ProgressFunc reports deployment progress (0-100) and a status message.
type ProgressFunc func(progress int, message string)

// Deployer handles SSH-based server deployment.
type Deployer struct{}

func NewDeployer() *Deployer {
	return &Deployer{}
}

// Deploy installs and starts the tunnel server on a remote host via SSH.
// SSH is assumed to be on port 22.
func (d *Deployer) Deploy(host, sshUser, sshPassword string, onProgress ProgressFunc) error {
	cfg := &ssh.ClientConfig{
		User:            sshUser,
		Auth:            []ssh.AuthMethod{ssh.Password(sshPassword)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}

	onProgress(5, "Connecting via SSH to "+host+"...")
	client, err := ssh.Dial("tcp", host+":22", cfg)
	if err != nil {
		return fmt.Errorf("SSH connection failed: %w", err)
	}
	defer client.Close()

	// run executes a shell command, returns combined output on error.
	run := func(cmd string) error {
		sess, err := client.NewSession()
		if err != nil {
			return err
		}
		defer sess.Close()
		out, err := sess.CombinedOutput(cmd)
		if err != nil {
			return fmt.Errorf("cmd %q failed: %s: %w", cmd, string(out), err)
		}
		return nil
	}

	// upload streams data into a remote file via stdin.
	upload := func(dst string, data []byte) error {
		sess, err := client.NewSession()
		if err != nil {
			return err
		}
		defer sess.Close()
		sess.Stdin = bytes.NewReader(data)
		out, err := sess.CombinedOutput("cat > " + dst)
		if err != nil {
			return fmt.Errorf("upload to %s failed: %s: %w", dst, string(out), err)
		}
		return nil
	}

	// uploadText writes a text file via base64 to avoid shell quoting issues.
	uploadText := func(dst, content string) error {
		encoded := base64.StdEncoding.EncodeToString([]byte(content))
		return run(fmt.Sprintf("printf '%%s' '%s' | base64 -d > %s", encoded, dst))
	}

	onProgress(10, "Creating directories...")
	if err := run("mkdir -p /opt/tunnel/data"); err != nil {
		return err
	}

	onProgress(20, "Installing dependencies...")
	err = run("bash -c 'apt-get install -y -q openssl 2>/dev/null || yum install -y openssl 2>/dev/null || true'")
	if err != nil {
		return fmt.Errorf("dependency install failed: %w", err)
	}

	onProgress(33, "Configuring firewall...")
	// ufw — правила персистентны по умолчанию
	run("bash -c 'command -v ufw &>/dev/null && ufw --force enable && ufw allow 22/tcp && ufw allow 443/tcp && ufw allow 8443/tcp || true'")
	// iptables — добавляем правила и сохраняем
	run("bash -c 'iptables -C INPUT -p tcp --dport 443 -j ACCEPT 2>/dev/null || iptables -A INPUT -p tcp --dport 443 -j ACCEPT || true'")
	run("bash -c 'iptables -C INPUT -p tcp --dport 8443 -j ACCEPT 2>/dev/null || iptables -A INPUT -p tcp --dport 8443 -j ACCEPT || true'")
	// Сохраняем правила iptables для выживания после reboot
	run("bash -c 'command -v iptables-save &>/dev/null && (mkdir -p /etc/iptables && iptables-save > /etc/iptables/rules.v4) || true'")
	run("bash -c 'command -v netfilter-persistent &>/dev/null && netfilter-persistent save || true'")
	// Если нет iptables-persistent — добавляем восстановление через rc.local
	run(`bash -c 'if ! command -v iptables-save &>/dev/null; then true; elif [ ! -f /etc/network/if-pre-up.d/iptables ]; then printf "#!/bin/sh\niptables-restore < /etc/iptables/rules.v4\n" > /etc/network/if-pre-up.d/iptables && chmod +x /etc/network/if-pre-up.d/iptables; fi || true'`)

	onProgress(42, "Uploading server binary...")
	serverBin, err := os.ReadFile(serverBinaryPath)
	if err != nil {
		return fmt.Errorf("server binary not found at %s — must run inside Docker container: %w", serverBinaryPath, err)
	}
	if err := upload("/opt/tunnel/tunnel-server", serverBin); err != nil {
		return fmt.Errorf("binary upload failed: %w", err)
	}
	if err := run("chmod +x /opt/tunnel/tunnel-server"); err != nil {
		return err
	}

	onProgress(60, "Writing server configuration...")
	serverConfig := `server:
  listen_port: 443
  metrics_port: 8443

tls:
  cert_path: /opt/tunnel/data/cert.pem
  key_path: /opt/tunnel/data/key.pem
  auto_cert: false

monitoring:
  enabled: true
  metrics_endpoint: /metrics
`

	if err := uploadText("/opt/tunnel/server.yml", serverConfig); err != nil {
		return fmt.Errorf("config upload failed: %w", err)
	}

	onProgress(70, "Generating TLS certificate...")
	err = run("bash -c '[ -f /opt/tunnel/data/cert.pem ] || openssl req -x509 -newkey rsa:4096 -keyout /opt/tunnel/data/key.pem -out /opt/tunnel/data/cert.pem -days 3650 -nodes -subj \"/CN=tunnel-server\" && chmod 600 /opt/tunnel/data/key.pem'")
	if err != nil {
		return fmt.Errorf("TLS cert generation failed: %w", err)
	}

	onProgress(82, "Installing systemd service...")
	serviceUnit := `[Unit]
Description=Tunnel Server
After=network.target

[Service]
ExecStart=/opt/tunnel/tunnel-server --config /opt/tunnel/server.yml
Restart=always
RestartSec=5
StandardOutput=append:/opt/tunnel/server.log
StandardError=append:/opt/tunnel/server.log

[Install]
WantedBy=multi-user.target
`
	if err := uploadText("/etc/systemd/system/tunnel-server.service", serviceUnit); err == nil {
		run("systemctl daemon-reload")
		run("systemctl stop tunnel-server 2>/dev/null || true")
		if err := run("systemctl enable --now tunnel-server"); err != nil {
			return fmt.Errorf("systemctl enable failed: %w", err)
		}
	} else {
		// No systemd — fall back to nohup.
		run("pkill -f 'tunnel-server --config' || true")
		if err := run("bash -c 'nohup /opt/tunnel/tunnel-server --config /opt/tunnel/server.yml > /opt/tunnel/server.log 2>&1 & disown && sleep 2'"); err != nil {
			return fmt.Errorf("failed to start server: %w", err)
		}
	}

	onProgress(93, "Verifying server is running...")
	time.Sleep(3 * time.Second)
	if err := run("pgrep -f 'tunnel-server --config' > /dev/null"); err != nil {
		return fmt.Errorf("server started but is not running — check /opt/tunnel/server.log")
	}

	onProgress(100, "Deployment complete!")
	return nil
}
