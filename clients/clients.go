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

// GetByAMQPURL Get by AMQP URL.
func GetByAMQPURL(amqpURL string) (*EndpointClients, bool) {
	poolMu.RLock()
	defer poolMu.RUnlock()
	for _, c := range pool {
		if c.EP.AMQP != nil && c.EP.AMQP.URL == amqpURL {
			return c, true
		}
	}
	return nil, false
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

func newAMQPClients(ep *config.ManagementEndpoint) (*amqp.Connection, *amqp.Channel, error) {
	if ep.AMQP == nil || ep.AMQP.URL == "" {
		return nil, nil, fmt.Errorf("missing amqp.URL")
	}
	au, err := url.Parse(ep.AMQP.URL)
	if err != nil {
		return nil, nil, err
	}
	if au.Scheme == "amqp" {
		conn, err := amqp.Dial(au.String())
		if err != nil {
			return nil, nil, err
		}
		ch, err := conn.Channel()
		if err != nil {
			conn.Close()
			return nil, nil, err
		}
		return conn, ch, nil
	}
	if au.Scheme == "amqps" {
		tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: ep.AMQP.InsecureSkipVerify}
		if ep.AMQP.CAFile != "" {
			pem, err := os.ReadFile(ep.AMQP.CAFile)
			if err != nil {
				return nil, nil, err
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return nil, nil, fmt.Errorf("invalid AMQP CA: %s", ep.AMQP.CAFile)
			}
			tlsCfg.RootCAs = pool
		}
		conn, err := amqp.DialTLS(au.String(), tlsCfg)
		if err != nil {
			return nil, nil, err
		}
		ch, err := conn.Channel()
		if err != nil {
			conn.Close()
			return nil, nil, err
		}
		return conn, ch, nil
	}
	return nil, nil, fmt.Errorf("unsupported amqp scheme: %s", au.Scheme)
}

// DialAMQPByMgmtURL dials AMQP on demand for the endpoint identified by the management URL.
func DialAMQPByMgmtURL(mgmtURL string) (*amqp.Connection, *amqp.Channel, error) {
	poolMu.RLock()
	c, ok := pool[mgmtURL]
	poolMu.RUnlock()
	if !ok || c == nil || c.EP == nil {
		return nil, nil, fmt.Errorf("unknown management endpoint: %s", mgmtURL)
	}
	return newAMQPClients(c.EP)
}

// DialAMQPByAMQPURL dials AMQP on demand for the endpoint that matches the given AMQP URL.
func DialAMQPByAMQPURL(amqpURL string) (*amqp.Connection, *amqp.Channel, error) {
	poolMu.RLock()
	defer poolMu.RUnlock()
	for _, c := range pool {
		if c.EP != nil && c.EP.AMQP != nil && c.EP.AMQP.URL == amqpURL {
			return newAMQPClients(c.EP)
		}
	}
	return nil, nil, fmt.Errorf("unknown amqp endpoint: %s", amqpURL)
}
