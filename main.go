/*
 * Copyright 2024 steadybit GmbH. All rights reserved.
 */

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	_ "github.com/KimMachineGun/automemlimit" // Sets GOMEMLIMIT to 90% of cgroup limit.
	rabbithole "github.com/michaelklishin/rabbit-hole/v3"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/action-kit/go/action_kit_sdk"
	"github.com/steadybit/advice-kit/go/advice_kit_api"
	"github.com/steadybit/discovery-kit/go/discovery_kit_api"
	"github.com/steadybit/discovery-kit/go/discovery_kit_sdk"
	"github.com/steadybit/event-kit/go/event_kit_api"
	"github.com/steadybit/extension-kit/extbuild"
	"github.com/steadybit/extension-kit/exthealth"
	"github.com/steadybit/extension-kit/exthttp"
	"github.com/steadybit/extension-kit/extlogging"
	"github.com/steadybit/extension-kit/extruntime"
	"github.com/steadybit/extension-kit/extsignals"
	"github.com/steadybit/extension-rabbitmq/config"
	"github.com/steadybit/extension-rabbitmq/extrabbitmq"
	_ "go.uber.org/automaxprocs" // Adjusts GOMAXPROCS.
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
)

// ExtensionListResponse merges ActionKit, DiscoveryKit, EventKit, AdviceKit.
type ExtensionListResponse struct {
	action_kit_api.ActionList       `json:",inline"`
	discovery_kit_api.DiscoveryList `json:",inline"`
	event_kit_api.EventListenerList `json:",inline"`
	advice_kit_api.AdviceList       `json:",inline"`
}

func main() {
	extlogging.InitZeroLog()
	extbuild.PrintBuildInformation()
	extruntime.LogRuntimeInformation(zerolog.DebugLevel)

	config.ParseConfiguration()
	config.ValidateConfiguration()
	testManagementConnection()

	exthealth.SetReady(false)
	exthealth.StartProbes(8084)

	ctx, cancel := SignalCanceledContext()
	registerHandlers(ctx)

	extsignals.AddSignalHandler(extsignals.SignalHandler{
		Handler: func(s os.Signal) { cancel() },
		Order:   extsignals.OrderStopCustom,
		Name:    "custom-extension-rabbitmq",
	})
	extsignals.ActivateSignalHandlers()

	action_kit_sdk.RegisterCoverageEndpoints()
	exthealth.SetReady(true)

	exthttp.Listen(exthttp.ListenOpts{Port: 8083})
}

func registerHandlers(ctx context.Context) {
	// Discovery
	discovery_kit_sdk.Register(extrabbitmq.NewRabbitVhostDiscovery(ctx))
	discovery_kit_sdk.Register(extrabbitmq.NewRabbitNodeDiscovery(ctx))
	discovery_kit_sdk.Register(extrabbitmq.NewRabbitQueueDiscovery(ctx))
	discovery_kit_sdk.Register(extrabbitmq.NewRabbitExchangeDiscovery(ctx))

	// Actions:
	action_kit_sdk.RegisterAction(extrabbitmq.NewProduceRabbitFixedAmount())

	// Root index
	exthttp.RegisterHttpHandler("/", exthttp.GetterAsHandler(getExtensionList))
}

func getExtensionList() ExtensionListResponse {
	return ExtensionListResponse{
		ActionList:    action_kit_sdk.GetActionList(),
		DiscoveryList: discovery_kit_sdk.GetDiscoveryList(),
	}
}

func SignalCanceledContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR1)
	go func() {
		select {
		case <-c:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, func() {
		signal.Stop(c)
		cancel()
	}
}

// --- connectivity check against the management API ---

func testManagementConnection() {
	endpoints := config.Config.ManagementEndpoints
	if len(endpoints) == 0 {
		log.Warn().Msg("no management endpoints configured; skipping connectivity check")
		return
	}

	okAny := false
	for _, ep := range endpoints {
		cl, err := newMgmtClient(ep)
		if err != nil {
			log.Error().Err(err).Str("endpoint", ep.URL).Msg("failed to init management client")
			continue
		}
		if _, err := cl.Overview(); err != nil {
			log.Error().Err(err).Str("endpoint", ep.URL).Msg("management API ping failed")
			continue
		}
		log.Info().Str("endpoint", ep.URL).Msg("management API reachable")
		okAny = true
	}
	if !okAny {
		log.Fatal().Msg("no reachable RabbitMQ management endpoint")
	}
}

func newMgmtClient(ep config.ManagementEndpoint) (*rabbithole.Client, error) {
	if ep.URL == "" {
		return nil, fmt.Errorf("endpoint URL is empty")
	}

	u, err := url.Parse(ep.URL)
	if err != nil {
		return nil, err
	}

	// Credentials: endpoint fields take precedence; fall back to URL userinfo if present
	user := ep.Username
	pass := ep.Password
	if (user == "" || pass == "") && u.User != nil {
		if ui := u.User.Username(); ui != "" && user == "" {
			user = ui
		}
		if pw, ok := u.User.Password(); ok && pass == "" {
			pass = pw
		}
	}

	// Plain HTTP or HTTPS without custom TLS
	if u.Scheme == "http" || (!ep.InsecureSkipVerify && ep.CAFile == "") {
		return rabbithole.NewClient(u.String(), user, pass)
	}

	// HTTPS with TLS options
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if ep.InsecureSkipVerify {
		tlsCfg.InsecureSkipVerify = true
	}
	if caPath := ep.CAFile; caPath != "" {
		pem, readErr := os.ReadFile(caPath)
		if readErr != nil {
			return nil, readErr
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("failed to parse CA bundle: %s", caPath)
		}
		tlsCfg.RootCAs = pool
	}

	tr := &http.Transport{TLSClientConfig: tlsCfg}
	return rabbithole.NewTLSClient(u.String(), user, pass, tr)
}
