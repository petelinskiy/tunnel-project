package deploy

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"log"
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
// SSH is assumed to be on port 22. authToken is written to the server config.
// tunnelPort is the port the tunnel server will listen on (default 443).
// domain, if non-empty, triggers certbot Let's Encrypt certificate issuance.
func (d *Deployer) Deploy(host, sshUser, sshPassword, authToken, domain string, tunnelPort int, onProgress ProgressFunc) error {
	if tunnelPort == 0 {
		tunnelPort = 443
	}
	metricsPort := tunnelPort + 1000
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
	err = run("bash -c 'apt-get install -y -q openssl 2>/dev/null || yum install -y openssl 2>/dev/null || pacman -S --noconfirm --needed openssl 2>/dev/null || true'")
	if err != nil {
		return fmt.Errorf("dependency install failed: %w", err)
	}

	// Firewall rules are managed by the server binary itself (openFirewall/closeFirewall)
	// to avoid touching the host's persistent firewall configuration.

	onProgress(42, "Stopping existing server...")
	run("systemctl stop tunnel-server 2>/dev/null || true")
	run("pkill -f 'tunnel-server --config' 2>/dev/null || true")

	onProgress(50, "Uploading server binary...")
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

	// Determine cert paths based on whether a domain was supplied
	certPath := "/opt/tunnel/data/cert.pem"
	keyPath := "/opt/tunnel/data/key.pem"
	if domain != "" {
		certPath = fmt.Sprintf("/etc/letsencrypt/live/%s/fullchain.pem", domain)
		keyPath = fmt.Sprintf("/etc/letsencrypt/live/%s/privkey.pem", domain)
	}

	onProgress(60, "Writing server configuration...")
	serverConfig := fmt.Sprintf(`auth_token: %s

server:
  listen_port: %d
  metrics_port: %d

tls:
  cert_path: %s
  key_path: %s
  auto_cert: false

monitoring:
  enabled: true
  metrics_endpoint: /metrics
`, authToken, tunnelPort, metricsPort, certPath, keyPath)

	if err := uploadText("/opt/tunnel/server.yml", serverConfig); err != nil {
		return fmt.Errorf("config upload failed: %w", err)
	}

	if domain != "" {
		onProgress(65, "Installing certbot...")
		run("bash -c 'apt-get install -y -q certbot 2>/dev/null || yum install -y certbot 2>/dev/null || snap install --classic certbot 2>/dev/null || true'")

		onProgress(70, "Obtaining Let's Encrypt certificate for "+domain+"...")
		// Open port 80 in iptables for ACME challenge (renewal also needs it)
		run("iptables -C INPUT -p tcp --dport 80 -j ACCEPT 2>/dev/null || iptables -A INPUT -p tcp --dport 80 -j ACCEPT")

		if err := run(fmt.Sprintf(
			"certbot certonly --standalone --non-interactive --agree-tos --email admin@%s -d %s",
			domain, domain,
		)); err != nil {
			return fmt.Errorf("certbot failed (check DNS A record for %s points to this server): %w", domain, err)
		}

		// Renewal cron: runs twice daily, restarts tunnel-server on new cert
		renewalCron := "0 0,12 * * * root certbot renew --quiet --standalone --deploy-hook 'systemctl restart tunnel-server 2>/dev/null || pkill -f tunnel-server'\n"
		if err := uploadText("/etc/cron.d/certbot-tunnel", renewalCron); err != nil {
			log.Printf("Warning: failed to install renewal cron: %v", err)
		}
	} else {
		onProgress(70, "Generating TLS certificate...")
		err = run("bash -c '[ -f /opt/tunnel/data/cert.pem ] || openssl req -x509 -newkey rsa:4096 -keyout /opt/tunnel/data/key.pem -out /opt/tunnel/data/cert.pem -days 3650 -nodes -subj \"/CN=tunnel-server\" && chmod 600 /opt/tunnel/data/key.pem'")
		if err != nil {
			return fmt.Errorf("TLS cert generation failed: %w", err)
		}
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

// Uninstall removes tunnel-server from a remote host via SSH.
func (d *Deployer) Uninstall(host, sshUser, sshPassword string) error {
	cfg := &ssh.ClientConfig{
		User:            sshUser,
		Auth:            []ssh.AuthMethod{ssh.Password(sshPassword)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}
	client, err := ssh.Dial("tcp", host+":22", cfg)
	if err != nil {
		return fmt.Errorf("SSH connection failed: %w", err)
	}
	defer client.Close()

	run := func(cmd string) {
		sess, err := client.NewSession()
		if err != nil {
			return
		}
		defer sess.Close()
		sess.CombinedOutput(cmd)
	}

	// Stop and remove service
	run("systemctl stop tunnel-server 2>/dev/null || true")
	run("systemctl disable tunnel-server 2>/dev/null || true")
	run("pkill -f 'tunnel-server --config' 2>/dev/null || true")
	run("rm -f /etc/systemd/system/tunnel-server.service")
	run("systemctl daemon-reload 2>/dev/null || true")
	run("rm -rf /opt/tunnel")

	// Firewall rules are intentionally NOT removed — the server may use
	// aapanel (BT), CSF, firewalld or other firewall managers that own
	// their own chains/rulesets. Touching iptables/nftables here risks
	// breaking SSH and panel access on shared/managed VPS hosts.

	return nil
}
