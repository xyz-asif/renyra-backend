
# Anchor Backend

The Go backend service for the Anchor content curation platform.

## Quick Start

### Prerequisites
- Go 1.24+
- MongoDB 5.0+ (Replica Set required for transactions)
- Firebase Project (Authentication enabled)

### Setup

1. **Configure Environment**
   ```bash
   cp .env.example .env
   # Edit .env with your MongoDB URI, Firebase credentials, and JWT secret
   ```

2. **Run Dependencies**
   Ensure MongoDB is running with replica set enabled (see [SETUP.md](SETUP.md) for details).

3. **Run Application**
   ```bash
   make run
   # or for development with hot-reload:
   make air
   ```

## Documentation

- [Setup Guide](SETUP.md): Detailed installation and configuration instructions.
- [API Documentation](docs/swagger.json): Swagger/OpenAPI spec (available at `/swagger/` when running).

## Commands

| Command | Description |
|:---|:---|
| `make build` | Build the binary to `bin/gotodo` |
| `make run` | Run the API server |
| `make air` | Run with hot-reload (requires `air`) |
| `make test` | Run tests |
| `make lint` | Run static analysis |

# renyra-backend
