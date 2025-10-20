/*
 * Copyright 2024 steadybit GmbH. All rights reserved.
 */

package main

import (
	"context"
	_ "github.com/KimMachineGun/automemlimit" // Sets GOMEMLIMIT to 90% of cgroup limit.
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
	"github.com/steadybit/extension-rabbitmq/clients"
	"github.com/steadybit/extension-rabbitmq/config"
	"github.com/steadybit/extension-rabbitmq/extrabbitmq"
	_ "go.uber.org/automaxprocs" // Adjusts GOMAXPROCS.
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

	exthealth.SetReady(false)
	exthealth.StartProbes(8084)

	err := clients.Init()
	if err != nil {
		// Proceed even if some endpoints failed to initialize. Pool may be partially available.
		log.Warn().Err(err).Msg("client initialization reported errors; continuing with partial pool")
	}

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
	//discovery_kit_sdk.Register(extrabbitmq.NewRabbitExchangeDiscovery(ctx))

	// Actions
	action_kit_sdk.RegisterAction(extrabbitmq.NewProduceRabbitFixedAmount())

	// Checks
	action_kit_sdk.RegisterAction(extrabbitmq.NewQueueBacklogCheckAction())

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
