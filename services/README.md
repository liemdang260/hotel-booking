# Service layout

All services use one root Go module and the same Clean Architecture scaffold:

```text
services/<service>/
├── cmd/server/                 # composition root and executable entry point
└── internal/
    ├── application/            # transport/worker entry-point adapters only
    ├── usecase/                # orchestration and transaction boundaries
    ├── domain/repository/      # inner-side repository/gateway contracts
    └── infrastructure/         # concrete external adapters
```

Dependency direction is inward: application calls usecases; usecases depend on domain and repository interfaces; infrastructure implements those interfaces. Usecase packages must not import infrastructure packages.

The initial scaffold contains no business behavior. Run `make build` or `make test` from the repository root to validate all service packages.
