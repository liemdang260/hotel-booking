# Local infrastructure

The local stack provides PostgreSQL, Redis, and a single-node Kafka broker for development.

## Ports and health checks

| Service | Host port | Container port | Health check |
| --- | ---: | ---: | --- |
| PostgreSQL | `5432` | `5432` | `pg_isready` |
| Redis | `6379` | `6379` | `redis-cli ping` |
| Kafka | `9092` | `9092` | Kafka broker API versions command |

Ports can be overridden in `.env`.

## Start

```bash
cd deployments
cp .env.example .env
make up
make ps
```

## Stop

```bash
cd deployments
make down
```

## Reset local data

This removes the named PostgreSQL, Redis, and Kafka volumes.

```bash
cd deployments
make reset
```

## Logs

```bash
cd deployments
make logs
```

The checked-in `.env.example` contains local-development defaults only. Do not commit real credentials or production secrets.
