# Steadybit extension-rabbitmq

A [Steadybit](https://www.steadybit.com/) extension to integrate [Rabbitmq](https://www.rabbitmq.com/) into Steadybit.

Learn about the capabilities of this extension in
our [Reliability Hub](https://hub.steadybit.com/extension/com.steadybit.extension_rabbitmq).

## Prerequisites

The extension-rabbitmq is using these capacities through management endpoint and ampq endpoint, thus may need elevated rights on rabbitmq side :

- List Queues
- Get Queue Metrics
- List Vhosts
- List Nodes
- Publish Messages
- Create / Delete Policies

## Configuration

| Environment Variable                                       | Helm value                                 | Meaning                                                                                                                                                                                               | Required | Default |
|------------------------------------------------------------|--------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|----------|---------|
| `STEADYBIT_EXTENSION_MANAGEMENT_ENDPOINTS_JSON`            | `rabbitmq.managementEndpoints`             | JSON array describing all RabbitMQ clusters and their management and AMQP endpoints. Each object must include `url`, `username`, `password`, and an `amqp` object with its own connection parameters. | yes      |         |
|                                                            |                                            | Example:<br>`[{"url":"https://mq-0.ns.svc:15672","username":"admin","password":"s3cr3t","amqp":{"url":"amqps://mq-0.ns.svc:5671","vhost":"/","insecureSkipVerify":false}}]`                           |          |         | | no       |         |
| `STEADYBIT_EXTENSION_DISCOVERY_INTERVAL_RABBIT_BROKER`     | `discovery.interval.rabbitBroker`          | Interval (in seconds) for discovering RabbitMQ cluster nodes.                                                                                                                                         | no       | `30`    |
| `STEADYBIT_EXTENSION_DISCOVERY_INTERVAL_RABBIT_VHOST`      | `discovery.interval.rabbitVhost`           | Interval (in seconds) for discovering RabbitMQ vhosts.                                                                                                                                                | no       | `30`    |
| `STEADYBIT_EXTENSION_DISCOVERY_INTERVAL_RABBIT_QUEUE`      | `discovery.interval.rabbitQueue`           | Interval (in seconds) for discovering RabbitMQ queues.                                                                                                                                                | no       | `30`    |
| `STEADYBIT_EXTENSION_DISCOVERY_ATTRIBUTES_EXCLUDES_VHOSTS` | `discovery.attributes.excludes.vhost`      | List of Vhost attributes to exclude during discovery. Checked by key equality and supporting trailing `"*"`.                                                                                          | no       |         |
| `STEADYBIT_EXTENSION_DISCOVERY_ATTRIBUTES_EXCLUDES_QUEUES` | `discovery.attributes.excludes.queue`      | List of Queue attributes to exclude during discovery. Checked by key equality and supporting trailing `"*"`.                                                                                          | no       |         |

The extension supports all environment variables provided
by [steadybit/extension-kit](https://github.com/steadybit/extension-kit#environment-variables).

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
