// clients/clients.go
package clients

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	rabbithole "github.com/michaelklishin/rabbit-hole/v3"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/steadybit/extension-rabbitmq/config"
	"net/http"
	"net/url"
	"os"
	"strings"
)

func CreateMgmtClientFromURL(config *config.ManagementEndpoint) (*rabbithole.Client, error) {
	if config.URL == "" {
		return nil, fmt.Errorf("empty management URL")
	}
	u, err := url.Parse(config.URL)
	if err != nil {
		return nil, err
	}
	if (config.Username == "" || config.Password == "") && u.User != nil {
		if uu := u.User.Username(); uu != "" && config.Username == "" {
			config.Username = uu
		}
		if pw, ok := u.User.Password(); ok && config.Password == "" {
			config.Password = pw
		}
	}
	if u.Scheme == "http" {
		return rabbithole.NewClient(u.String(), config.Username, config.Password)
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: config.InsecureSkipVerify}
	if config.CAFile != "" {
		pem, err := os.ReadFile(config.CAFile)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("invalid CA: %s", config.CAFile)
		}
		tlsCfg.RootCAs = pool
	}
	tr := &http.Transport{TLSClientConfig: tlsCfg}
	return rabbithole.NewTLSClient(u.String(), config.Username, config.Password, tr)
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
