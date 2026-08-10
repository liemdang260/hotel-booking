# Local infrastructure

The local dependency stack contains PostgreSQL, Redis, and a single-node Kafka broker in KRaft mode. It is intended for development only.

## Ports and health

| Service | Host port | Health check |
| --- | ---: | --- |
| PostgreSQL | `5432` | `pg_isready` |
| Redis | `6379` | `redis-cli ping` |
| Kafka | `9092` | Kafka topic listing through the local broker |

Ports can be overridden with environment variables from `deployments/.env.example`.

## Start

```bash
cp deployments/.env.example deployments/.env
make infra-up
```

## Inspect

```bash
make infra-ps
make infra-logs
```

## Stop

```bash
make infra-down
```

## Reset all local data

```bash
make infra-reset
```

`infra-reset` removes the named PostgreSQL, Redis, and Kafka volumes before the next startup. Do not place production credentials in the environment template or commit a populated `.env` file.
