# CRM Backend

Go REST API for the CRM platform.

## Requirements

- Go 1.22+
- PostgreSQL 14+

## Setup

```bash
cp .env.example .env
# Edit DATABASE_URL and JWT_SECRET
```

## Run

```bash
go run ./cmd/api
```

Migrations run automatically on startup.

## Health

- `GET /api/health`
- `GET /api/v1/health`

## Tests

```bash
go test ./...
```
