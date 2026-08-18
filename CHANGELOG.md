# Changelog

## v1.1.1

- chore(deps): bump steadybit kits and drop Go patch pin (#49)
- chore: deprecate the exchange parameter of the queue publish attacks (#48)

## v1.1.1

- chore: deprecate the exchange parameter of the queue publish attacks — use the Publish to Exchange actions to publish via an exchange, or recreate the step if no exchange is needed. The parameter keeps working; saved experiments are unaffected.

## v1.1.0

- feat: discover RabbitMQ exchanges as targets (excluding the default exchange, `amq.*` built-ins and internal exchanges)
- feat: fetch exchanges paged and column-filtered so brokers with thousands of exchanges are discovered in bounded chunks
- feat: new attacks "Publish to Exchange (# of Messages)" and "Publish to Exchange (Messages / s)" — delivery is determined by the exchange type and its bindings; unroutable messages count as failures
- feat: fail the prepare step of the queue publish attacks when the exchange parameter is set and more than 10 queue targets publish to the same exchange and routing key, preventing accidental load amplification
- fix: reject numberOfMessages = 0 at prepare instead of completing instantly with a confusing 0% success rate
- fix: wait for in-flight publish confirmations when an attack stops, so the success-rate verdict no longer misses the last message's confirm (e.g. 119/120 on a fully successful run)
- fix: log a warning when the cluster name cannot be resolved during discovery instead of silently reporting an empty `rabbitmq.cluster.name`

## v1.0.21

- feat: support filtering targets out of discovery
- fix: emit the node check metric immediately on Start (#42)
- fix: emit the queue backlog metric immediately on Start (#43)

## v1.0.20

- chore(deps): update dependencies

## v1.0.19

- chore(deps): bump github.com/rabbitmq/amqp091-go from 1.12.0 to 1.13.0

## v1.0.18

- Add a "Fail early" option to the node check and the queue backlog check. When enabled, the check fails as soon as a deviating event is observed (node check: a deviating change; queue backlog check: the backlog exceeding the threshold), instead of waiting for the end of the step. The node check defaults to fail-early (matching its previous "All the time" behavior); the queue backlog check defaults to fail-at-end (matching its previous behavior). The node check option only affects the "All the time" mode.
- chore(deps): bump go to 1.26.5 (#39)
- ci: skip build on .trivyignore.yml-only changes [skip ci]
- feat(checks): add fail early option (#38)
- refactor: register extension index via exthttp.RegisterRevisionedHandler (#40)

## v1.0.17

- Merge pull request #32 from steadybit/feat/add-claude-workflows
- chore(deps): bump github.com/steadybit/action-kit/go/action_kit_sdk
- chore(deps): bump github.com/steadybit/discovery-kit/go/discovery_kit_sdk
- chore(deps): bump github.com/steadybit/event-kit/go/event_kit_api
- chore(deps): bump github.com/steadybit/extension-kit
- chore: silence SonarQube finding on secrets: inherit in Claude workflows
- fix: guard the publish attack's jobs channel against being closed twice when stop runs concurrently/twice, which could panic the extension
- fix: prevent double-close panic on the publish jobs channel (#33)

## v1.0.16

- chore(deps): bump github.com/rabbitmq/amqp091-go from 1.11.0 to 1.12.0
- chore(deps): bump github.com/steadybit/extension-kit
- chore(deps): bump golang.org/x/net to v0.55.0 (CVE-2026-39821) (#27)

## v1.0.15

- chore(deps): bump alpine from 3.23 to 3.24

## v1.0.14

- chore: update to go 1.26.4
- feat: add weekly auto patch-release workflow

## v1.0.13

- Support discovery group attribute via `STEADYBIT_EXTENSION_DISCOVERY_GROUP` env var (or `discovery.group` Helm value) — when set, the extension adds `steadybit.group=<value>` to every discovered target
- Update dependencies

## v1.0.12

- Bump Go to 1.26.3
- Update dependencies
- Improved action descriptions

## v1.0.11

- Bump Go to 1.25.9
- Support if-none-match for the extension list endpoint
- Update dependencies

## v1.0.10

- fix: prevent deadlock in publish stop when AMQP workers die
- fix: prevent send on closed channel panic and reduce queue discovery overhead
- fix: less details in logs when workers are involved
- Update alpine packages in Docker image to address CVEs
- Update dependencies

## v1.0.7

- Update dependencies

## v1.0.6

- Update dependencies

## v1.0.5

- Update dependencies

## v1.0.4

## v1.0.3

- Update dependencies

## v1.0.2

## v1.0.1

## v1.0.0

 - Initial release
