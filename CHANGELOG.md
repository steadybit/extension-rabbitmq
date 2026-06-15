# Changelog

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
