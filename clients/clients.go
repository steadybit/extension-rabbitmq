package clients

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	rabbithole "github.com/michaelklishin/rabbit-hole/v3"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/steadybit/extension-rabbitmq/config"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type retryTransport struct {
	base       http.RoundTripper
	maxRetries int
	backoff    time.Duration
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.base == nil {
		t.base = http.DefaultTransport
	}

	// Only retry idempotent, usually safe management calls
	if req.Method != http.MethodGet && req.Method != http.MethodHead && req.Method != http.MethodOptions {
		return t.base.RoundTrip(req)
	}

	var resp *http.Response
	var err error

	for attempt := 0; attempt <= t.maxRetries; attempt++ {
		resp, err = t.base.RoundTrip(req)
		if err == nil && resp != nil && resp.StatusCode != http.StatusGatewayTimeout {
			return resp, nil
		}

		// Stop if this was the last allowed attempt
		if attempt == t.maxRetries {
			break
		}

		// Simple fixed backoff before retrying
		if t.backoff > 0 {
			time.Sleep(t.backoff)
		}
	}

	return resp, err
}

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
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
	rt, err := newMgmtTransport(config)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "http" {
		client, err := rabbithole.NewClient(u.String(), config.Username, config.Password)
		if err != nil {
			return nil, err
		}
		client.SetTransport(rt)
		return client, nil
	}
	return rabbithole.NewTLSClient(u.String(), config.Username, config.Password, rt)
}

// newMgmtTransport builds the retrying (and, for https, TLS-configured) transport used for all
// management API calls of an endpoint.
func newMgmtTransport(cfg *config.ManagementEndpoint) (http.RoundTripper, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, err
	}
	base := http.RoundTripper(http.DefaultTransport)
	if u.Scheme == "https" {
		tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: cfg.InsecureSkipVerify}
		if cfg.CAFile != "" {
			pem, err := os.ReadFile(cfg.CAFile)
			if err != nil {
				return nil, err
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return nil, fmt.Errorf("invalid CA: %s", cfg.CAFile)
			}
			tlsCfg.RootCAs = pool
		}
		base = &http.Transport{TLSClientConfig: tlsCfg}
	}
	return &retryTransport{base: base, maxRetries: 2, backoff: 500 * time.Millisecond}, nil
}

// pagedExchanges mirrors the management API's paged envelope for /api/exchanges.
type pagedExchanges struct {
	Items      []rabbithole.ExchangeInfo `json:"items"`
	Page       int                       `json:"page"`
	PageCount  int                       `json:"page_count"`
	TotalCount int                       `json:"total_count"`
}

// exchangeColumns limits the response to the attributes the discovery reports. Without it the
// management API includes per-exchange rate statistics, which multiplies the payload on
// brokers with thousands of exchanges.
const exchangeColumns = "name,vhost,type,durable,auto_delete,internal"

// ListAllExchanges lists the exchanges of a management endpoint page by page with a columns
// filter. rabbit-hole only offers an unpaged ListExchanges without column selection, which
// does not scale to brokers with thousands of exchanges.
func ListAllExchanges(cfg *config.ManagementEndpoint) ([]rabbithole.ExchangeInfo, error) {
	client, err := CreateMgmtClientFromURL(cfg)
	if err != nil {
		return nil, err
	}
	rt, err := newMgmtTransport(cfg)
	if err != nil {
		return nil, err
	}
	httpClient := &http.Client{Transport: rt, Timeout: 60 * time.Second}

	all := make([]rabbithole.ExchangeInfo, 0, 256)
	page := 1
	pageSize := 500
	for {
		u := fmt.Sprintf("%s/api/exchanges?page=%d&page_size=%d&columns=%s", strings.TrimSuffix(client.Endpoint, "/"), page, pageSize, url.QueryEscape(exchangeColumns))
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.SetBasicAuth(client.Username, client.Password)

		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		var paged pagedExchanges
		decodeErr := json.NewDecoder(resp.Body).Decode(&paged)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("listing exchanges failed: %s returned status %d", client.Endpoint, resp.StatusCode)
		}
		if decodeErr != nil {
			return nil, decodeErr
		}

		all = append(all, paged.Items...)
		if len(paged.Items) == 0 || len(all) >= paged.TotalCount || page >= paged.PageCount {
			return all, nil
		}
		page++
	}
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
