# Kafka event contracts

The source of truth for event delivery guarantees is PR #14. Contracts are
versioned independently from service domain models and use the common
`hotelbooking.events.common.v1.EventEnvelope`.

| Topic | Event type | Version | Payload |
| --- | --- | ---: | --- |
| `booking.events.v1` | `BookingConfirmed` | 1 | `hotelbooking.events.booking.v1.BookingConfirmedV1` |
| `availability.events.v1` | `ReservationExpired` | 1 | `hotelbooking.events.availability.v1.ReservationExpiredV1` |

The Kafka key is `aggregate_id`. The envelope `payload` is the serialized
event-specific Protobuf message. Dates in v1 payloads are ISO-8601 calendar
dates (`YYYY-MM-DD`). Monetary values are integer minor units paired with an
ISO-4217 currency code.

Existing message meanings and field numbers are immutable. Breaking changes
require a new event version and a new payload message. Consumers reject
unsupported versions instead of guessing. Duplicate `event_id` values are
expected under at-least-once delivery.

Notification consumes both v1 events through an application adapter. Its
usecase atomically records the event ID and creates one durable notification
job. Kafka offsets may be committed only after that transaction succeeds.
