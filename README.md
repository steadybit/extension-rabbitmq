# Steadybit extension-rabbitmq

A [Steadybit](https://www.steadybit.com/) extension to integrate [Rabbitmq](https://www.rabbitmq.com/) into Steadybit.

Learn about the capabilities of this extension in
our [Reliability Hub](https://hub.steadybit.com/extension/com.steadybit.extension_rabbitmq).

## Prerequisites

The extension-rabbitmq is using these capacities through management endpoint and ampq endpoint, thus may need elevated
rights on rabbitmq side :

- List Queues
- Get Queue Metrics
- List Vhosts
- List Nodes
- List Exchanges
- Publish Messages
- Create / Delete Policies

## Configuration

| Environment Variable                                       | Helm value                            | Meaning                                                                                                                                                                                               | Required | Default |
|------------------------------------------------------------|---------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|----------|---------|
| `STEADYBIT_EXTENSION_MANAGEMENT_ENDPOINTS_JSON`            | `rabbitmq.managementEndpoints`        | JSON array describing all RabbitMQ clusters and their management and AMQP endpoints. Each object must include `url`, `username`, `password`, and an `amqp` object with its own connection parameters. | yes      |         |
|                                                            |                                       | Example:<br>`[{"url":"https://mq-0.ns.svc:15672","username":"admin","password":"s3cr3t","amqp":{"url":"amqps://mq-0.ns.svc:5671","vhost":"/","insecureSkipVerify":false}}]`                           |          |         | | no       |         |
| `STEADYBIT_EXTENSION_DISCOVERY_INTERVAL_VHOST_SECONDS`     | `discovery.interval.vhost`            | Interval (in seconds) for discovering RabbitMQ cluster nodes.                                                                                                                                         | no       | `30`    |
| `STEADYBIT_EXTENSION_DISCOVERY_INTERVAL_NODE_SECONDS`      | `discovery.interval.node`             | Interval (in seconds) for discovering RabbitMQ vhosts.                                                                                                                                                | no       | `30`    |
| `STEADYBIT_EXTENSION_DISCOVERY_INTERVAL_QUEUE_SECONDS`     | `discovery.interval.queue`            | Interval (in seconds) for discovering RabbitMQ queues.                                                                                                                                                | no       | `120`   |
| `STEADYBIT_EXTENSION_DISCOVERY_INTERVAL_EXCHANGE_SECONDS`  | `discovery.interval.exchange`         | Interval (in seconds) for discovering RabbitMQ exchanges. The unnamed default exchange, `amq.*` built-ins and internal exchanges are never reported. Exchanges are near-static, so on brokers with thousands of exchanges consider raising this to several minutes and narrowing the reported targets with `discovery.excludeQuery`/`discovery.includeQuery`. | no       | `120`   |
| `STEADYBIT_EXTENSION_DISCOVERY_ATTRIBUTES_EXCLUDES_VHOSTS` | `discovery.attributes.excludes.vhost` | List of Vhost attributes to exclude during discovery. Checked by key equality and supporting trailing `"*"`.                                                                                          | no       |         |
| `STEADYBIT_EXTENSION_DISCOVERY_ATTRIBUTES_EXCLUDES_QUEUES` | `discovery.attributes.excludes.queue` | List of Queue attributes to exclude during discovery. Checked by key equality and supporting trailing `"*"`.                                                                                          | no       |         |
| `STEADYBIT_EXTENSION_DISCOVERY_ATTRIBUTES_EXCLUDES_EXCHANGES` | `discovery.attributes.excludes.exchange` | List of Exchange attributes to exclude during discovery. Checked by key equality and supporting trailing `"*"`.                                                                                 | no       |         |

Beyond the settings above, this extension supports the configuration common to all Steadybit
extensions:

- [extension-kit](https://github.com/steadybit/extension-kit#environment-variables) — HTTP and
  health ports, TLS and mutual TLS, unix domain socket, logging, and pprof.
- [Target Filtering](https://github.com/steadybit/discovery-kit/blob/main/docs/target-filtering.md) —
  stop the extension reporting targets you do not want.
- [Group Matching](https://github.com/steadybit/discovery-kit/blob/main/docs/target-enrichment.md#group-matching) —
  tag discovered targets with a group, so enrichment rules only match within it.

## Installation

### Using Docker

```sh
docker run \
  --rm \
  -p 8083 \
  --name steadybit-extension-rabbitmq \
  --env STEADYBIT_EXTENSION_MANAGEMENT_ENDPOINTS_JSON='[{"url":"http://localhost:15672","username":"guest","password":"guest","amqp":{"url":"amqp://localhost:5672","vhost":"/"}}]' \
  ghcr.io/steadybit/extension-rabbitmq:latest
```

### Using Helm in Kubernetes

```sh
helm repo add steadybit-extension-rabbitmq https://steadybit.github.io/extension-rabbitmq
helm repo update

helm upgrade steadybit-extension-rabbitmq \
  --install \
  --wait \
  --timeout 5m0s \
  --create-namespace \
  --namespace steadybit-agent \
  --set 'rabbitmq.managementEndpoints=[{"url":"http://localhost:15672","username":"guest","password":"guest","amqp":{"url":"amqp://localhost:5672","vhost":"/"}}]' \
  steadybit-extension-rabbitmq/steadybit-extension-rabbitmq
```

## Register the extension

Make sure to register the extension on the Steadybit platform. Please refer to
the [documentation](https://docs.steadybit.com/integrate-with-steadybit/extensions/extension-installation) for more
information.

---

## Version and Revision

The version and revision of the extension:

- are printed during the startup of the extension
- are added as a Docker label to the image
- are available via the `version.txt`/`revision.txt` files in the root of the image
