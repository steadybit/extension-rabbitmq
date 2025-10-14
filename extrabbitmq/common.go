package extrabbitmq

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	rabbithole "github.com/michaelklishin/rabbit-hole/v3"
	"github.com/steadybit/extension-rabbitmq/config"
)

func createNewClient(host string) (*rabbithole.Client, error) {
	// Accept plain hostnames by defaulting to http
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "http://" + host
	}

	u, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("invalid host URL: %w", err)
	}

	// Resolve credentials: prefer explicit config, otherwise from URL userinfo, otherwise empty
	username := config.Config.Username
	password := config.Config.Password
	if username == "" && u.User != nil {
		username = u.User.Username()
		if pw, ok := u.User.Password(); ok {
			password = pw
		}
	}

	// If scheme is http, use a simple client with whatever creds we have (possibly empty)
	if u.Scheme == "http" {
		return rabbithole.NewClient(u.String(), username, password)
	}

	// HTTPS. Use default transport when no custom TLS behavior is requested.
	if !config.Config.InsecureSkipVerify && config.Config.RabbitClusterCertChainFile == "" {
		return rabbithole.NewClient(u.String(), username, password)
	}

	// Build TLS config for custom verification settings or custom CA bundle.
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if config.Config.InsecureSkipVerify {
		tlsCfg.InsecureSkipVerify = true
	}

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
	return rabbithole.NewTLSClient(u.String(), username, password, transport)
}
