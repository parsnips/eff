# eff

Integration test scaffolding for the [Twisp](https://twisp.com) ledger using [testcontainers-go](https://golang.testcontainers.org/) and [genqlient](https://github.com/Khan/genqlient).

## Prerequisites

- Go 1.25+
- Docker

## Usage

Regenerate the GraphQL client after editing `operations.graphql`:

```bash
go generate ./...
```

Run integration tests (spins up a Twisp container via Docker):

```bash
go test -v -count=1 -timeout=300s ./...
```

Test the parallel test with 100 simultanous tests.
```
RUNS=100 go test -run ^TestParallel$ -v ./...
```

## Project Structure

| File                 | Description                                                   |
|----------------------|---------------------------------------------------------------|
| `schema.graphql`     | Twisp schema from introspection                               |
| `operations.graphql` | GraphQL mutations and queries                                 |
| `genqlient.yaml`     | genqlient configuration with scalar bindings                  |
| `scalars.go`         | Go types for Twisp custom scalars (UUID, Date, Decimal, etc.) |
| `generate.go`        | `//go:generate` directive                                     |
| `generated.go`       | genqlient output (auto-generated)                             |
| `twisp.go`           | testcontainers helper: `StartTwisp()`, `NewGraphQLClient()`   |
| `twisp_test.go`      | Integration tests                                             |
