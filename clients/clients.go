// clients/clients.go
package clients

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	rabbithole "github.com/michaelklishin/rabbit-hole/v3"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog/log"
	"github.com/steadybit/extension-rabbitmq/config"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
)

type EndpointClients struct {
	EP   *config.ManagementEndpoint
	Mgmt *rabbithole.Client
}

var (
	once   sync.Once
	poolMu sync.RWMutex
	pool   = map[string]*EndpointClients{} // key: mgmt URL
)

// Init Call once at startup after config.ParseConfiguration().
func Init() error {
	var initErr error
	once.Do(func() {
		for i := range config.Config.ManagementEndpoints {
			ep := &config.Config.ManagementEndpoints[i]
			c, err := buildClients(ep)
			if err != nil {
				log.Warn().Err(err).Str("endpoint", ep.URL).Msg("init: failed to build clients")
				if initErr == nil {
					initErr = err
				}
				continue
			}
			poolMu.Lock()
			pool[ep.URL] = c
			poolMu.Unlock()
		}
	})
	return initErr
}

// GetByMgmtURL Get by management URL.
func GetByMgmtURL(mgmtURL string) (*EndpointClients, bool) {
	poolMu.RLock()
	defer poolMu.RUnlock()
	c, ok := pool[mgmtURL]
	return c, ok
}

// --- internals ---

func buildClients(ep *config.ManagementEndpoint) (*EndpointClients, error) {
	mgmt, err := newMgmtClient(ep)
	if err != nil {
		return nil, err
	}
	return &EndpointClients{EP: ep, Mgmt: mgmt}, nil
}

func newMgmtClient(ep *config.ManagementEndpoint) (*rabbithole.Client, error) {
	if ep.URL == "" {
		return nil, fmt.Errorf("empty management URL")
	}
	u, err := url.Parse(ep.URL)
	if err != nil {
		return nil, err
	}
	user := ep.Username
	pass := ep.Password
	if (user == "" || pass == "") && u.User != nil {
		if uu := u.User.Username(); uu != "" && user == "" {
			user = uu
		}
		if pw, ok := u.User.Password(); ok && pass == "" {
			pass = pw
		}
	}
	if u.Scheme == "http" {
		return rabbithole.NewClient(u.String(), user, pass)
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: ep.InsecureSkipVerify}
	if ep.CAFile != "" {
		pem, err := os.ReadFile(ep.CAFile)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("invalid CA: %s", ep.CAFile)
		}
		tlsCfg.RootCAs = pool
	}
	tr := &http.Transport{TLSClientConfig: tlsCfg}
	return rabbithole.NewTLSClient(u.String(), user, pass, tr)
}

func CreateNewAMQPConnection(amqpUrl string, user, pass string, insecure bool, ca string) (*amqp.Connection, *amqp.Channel, error) {
	if strings.TrimSpace(amqpUrl) == "" {
		return nil, nil, fmt.Errorf("amqp url is empty")
	}

	au, err := url.Parse(amqpUrl)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid amqp url: %w", err)
	}

	// Inject credentials if provided and URL has none
	if (user != "" || pass != "") && au.User == nil {
		au.User = url.UserPassword(user, pass)
	}

	switch au.Scheme {
	case "amqp":
		conn, err := amqp.Dial(au.String())
		if err != nil {
			return nil, nil, err
		}
		ch, err := conn.Channel()
		if err != nil {
			_ = conn.Close()
			return nil, nil, err
		}
		return conn, ch, nil

	case "amqps":
		tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
		if insecure {
			tlsCfg.InsecureSkipVerify = true
		}
		if ca != "" {
			pemBytes, err := os.ReadFile(ca)
			if err != nil {
				return nil, nil, err
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pemBytes) {
				return nil, nil, fmt.Errorf("invalid CA: %s", ca)
			}
			tlsCfg.RootCAs = pool
		}
		conn, err := amqp.DialTLS(au.String(), tlsCfg)
		if err != nil {
			return nil, nil, err
		}
		ch, err := conn.Channel()
		if err != nil {
			_ = conn.Close()
			return nil, nil, err
		}
		return conn, ch, nil

	default:
		return nil, nil, fmt.Errorf("unsupported amqp scheme: %s", au.Scheme)
	}
}
