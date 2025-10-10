package extrabbitmq

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/url"
	"os"

	rabbithole "github.com/michaelklishin/rabbit-hole/v3"
	"github.com/steadybit/extension-rabbitmq/config"
)

func createNewClient(host string) (*rabbithole.Client, error) {
	// Expect host to include scheme and port, e.g. "http://127.0.0.1:15672" or "https://rmq.example.com:15671"
	// If HTTPS and custom TLS is required, configure a transport and use NewTLSClient.

	u, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("invalid host URL: %w", err)
	}

	// If no custom TLS behavior is requested or scheme is plain http, the simple client is enough.
	if u.Scheme == "http" || (!config.Config.InsecureSkipVerify && config.Config.RabbitClusterCertChainFile == "") {
		return rabbithole.NewClient(host, config.Config.Username, config.Config.Password)
	}

	// Build TLS config for HTTPS with custom verification settings or custom CA bundle.
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if config.Config.InsecureSkipVerify {
		tlsCfg.InsecureSkipVerify = true
	}

	// Treat RabbitClusterCertChainFile as a PEM-encoded CA bundle for server verification if provided.
	if config.Config.RabbitClusterCertChainFile != "" {
		pemBytes, readErr := os.ReadFile(config.Config.RabbitClusterCertChainFile)
		if readErr != nil {
			return nil, fmt.Errorf("read CA bundle: %w", readErr)
		}
		pool := x509.NewCertPool()
		if ok := pool.AppendCertsFromPEM(pemBytes); !ok {
			return nil, fmt.Errorf("failed to parse CA bundle in %s", config.Config.RabbitClusterCertChainFile)
		}
		tlsCfg.RootCAs = pool
	}

	transport := &http.Transport{TLSClientConfig: tlsCfg}
	return rabbithole.NewTLSClient(host, config.Config.Username, config.Config.Password, transport)
}
