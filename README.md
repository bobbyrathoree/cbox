<p align="center">
  <pre>
    ██████╗██████╗  ██████╗ ██╗  ██╗
   ██╔════╝██╔══██╗██╔═══██╗╚██╗██╔╝
   ██║     ██████╔╝██║   ██║ ╚███╔╝
   ██║     ██╔══██╗██║   ██║ ██╔██╗
   ╚██████╗██████╔╝╚██████╔╝██╔╝ ██╗
    ╚═════╝╚═════╝  ╚═════╝ ╚═╝  ╚═╝
  </pre>
  <strong>Container workflows that just work.</strong>
</p>

<p align="center">
  <a href="#installation">Installation</a> •
  <a href="#quick-start">Quick Start</a> •
  <a href="#zero-config">Zero-Config</a> •
  <a href="#commands">Commands</a> •
  <a href="#configuration">Configuration</a>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/version-0.7.0-blue.svg" alt="Version">
  <img src="https://img.shields.io/badge/go-%3E%3D1.21-00ADD8.svg" alt="Go Version">
  <img src="https://img.shields.io/badge/docker-%3E%3D23.0-2496ED.svg" alt="Docker Version">
  <img src="https://img.shields.io/badge/license-MIT-green.svg" alt="License">
</p>

---

## Why cbox?

Docker Compose is powerful but verbose. **cbox** is a fast, opinionated container workflow engine that auto-detects your project and generates sensible defaults.

```bash
# That's it. No config file needed.
cd my-fastapi-app
cbox up
```

<table>
<tr>
<td width="50%">

**Before (docker-compose.yml)**
```yaml
version: "3.8"
services:
  app:
    build:
      context: .
      dockerfile: Dockerfile
    ports:
      - "8000:8000"
    volumes:
      - .:/app
    environment:
      - PYTHONUNBUFFERED=1
    command: uvicorn main:app --reload
    depends_on:
      db:
        condition: service_healthy
  db:
    image: postgres:16-alpine
    environment:
      POSTGRES_PASSWORD: secret
    healthcheck:
      test: ["CMD", "pg_isready"]
      interval: 5s
```

</td>
<td width="50%">

**After (cbox.yaml)**
```yaml
version: "1"
services:
  app:
    path: .
    port: 8000
    depends_on: [db]
  db:
    image: postgres:16-alpine
    env:
      POSTGRES_PASSWORD: secret
```

</td>
</tr>
</table>

### Key Features

| Feature | Description |
|---------|-------------|
| **Zero-Config Mode** | Works without any config file for simple projects |
| **Smart Detection** | Auto-detects Node.js, Python, Go runtimes and frameworks |
| **Fast Builds** | BuildKit caching enabled by default |
| **Dev Mode** | Hot reload with `cbox dev` |
| **Port Conflict Resolution** | Automatically finds alternative ports |
| **Database Tools** | `cbox db shell`, snapshots, and more |
| **Cloud Deploy** | Push to ECR and deploy to ECS/Fargate |
| **Built-in Diagnostics** | `cbox diagnose` finds common problems |

---

## Installation

### Using Go

```bash
go install github.com/bobbyrathore/cbox/cmd/cbox@latest
```

### From Source

```bash
git clone https://github.com/bobbyrathore/cbox
cd cbox
make build
# Binary at ./bin/cbox
```

### Verify Installation

```bash
cbox version
cbox doctor  # Check system requirements
```

---

## Quick Start

```bash
# Initialize in any project directory
cbox init

# Start all services (detached)
cbox up -d

# View logs
cbox logs

# Check status
cbox ps

# Stop everything
cbox down
```

---

## Zero-Config

cbox can run without any configuration file. Just navigate to your project and run:

```bash
cbox build && cbox up
```

### Supported Runtimes

| Runtime | Detection | Frameworks |
|---------|-----------|------------|
| **Node.js** | `package.json` | Express, Fastify, NestJS, Next.js, Remix, Vite, Astro |
| **Python** | `requirements.txt`, `pyproject.toml` | FastAPI, Flask, Django, Starlette |
| **Go** | `go.mod` | Gin, Echo, Fiber, Chi, net/http |

### Example: FastAPI Zero-Config

```bash
# Create a minimal FastAPI app
mkdir my-api && cd my-api

cat > main.py << 'EOF'
from fastapi import FastAPI
app = FastAPI()

@app.get("/")
def read_root():
    return {"message": "Hello, World!"}
EOF

echo "fastapi\nuvicorn" > requirements.txt

# Build and run - no config needed!
cbox build
# → Auto-detected: Python (FastAPI)
# ✓ Built app in 12.3s → my-api-app:latest

cbox up -d
curl http://localhost:8000
# {"message": "Hello, World!"}
```

---

## Commands

### Core Workflow

| Command | Description |
|---------|-------------|
| `cbox init` | Generate cbox.yaml from project detection |
| `cbox build [service...]` | Build Docker images |
| `cbox up [-d]` | Start services (use `-d` for detached) |
| `cbox down` | Stop and remove containers |
| `cbox restart [service]` | Restart services |

### Development

| Command | Description |
|---------|-------------|
| `cbox dev` | Start in development mode with hot reload |
| `cbox logs [-f] [service]` | View logs (use `-f` to follow) |
| `cbox exec <service> -- <cmd>` | Execute command in running container |
| `cbox run <service> -- <cmd>` | Run one-off command in new container |

### Monitoring & Debugging

| Command | Description |
|---------|-------------|
| `cbox ps` | List running services |
| `cbox top [service]` | Show running processes |
| `cbox validate` | Validate configuration |
| `cbox diagnose` | Find common problems |
| `cbox doctor` | Check system requirements |
| `cbox dashboard` | Open web dashboard |

### Database Tools

| Command | Description |
|---------|-------------|
| `cbox db shell <service>` | Open database shell |
| `cbox db snapshot <service>` | Create database snapshot |
| `cbox db restore <service> <file>` | Restore from snapshot |

### Deployment

| Command | Description |
|---------|-------------|
| `cbox push` | Push images to registry |
| `cbox deploy` | Deploy to cloud (ECS/Fargate) |
| `cbox tunnel <service>` | Create secure tunnel to service |

### Utilities

| Command | Description |
|---------|-------------|
| `cbox clean` | Remove stopped containers and unused images |
| `cbox wait <service>` | Wait for service to be healthy |

---

## Configuration

### Minimal Configuration

```yaml
version: "1"
services:
  app:
    path: .
    port: 3000
```

### Full Configuration Reference

```yaml
version: "1"

project:
  name: my-app  # Defaults to directory name

services:
  api:
    # Source (one required)
    path: ./api              # Build from source
    # image: nginx:alpine    # OR use pre-built image

    # Runtime (auto-detected if omitted)
    runtime: nodejs          # nodejs, python, go

    # Container
    port: 3000               # Primary port
    expose: [3000, 9229]     # Additional ports
    command: ["npm", "start"]

    # Build options
    build:
      dockerfile: Dockerfile.custom
      target: production
      args:
        NODE_ENV: production

    # Development mode
    dev:
      command: ["npm", "run", "dev"]
      sync: true             # Enable file sync
      watch:
        paths: ["src/", "package.json"]
        ignore: ["node_modules/", "dist/"]

    # Dependencies
    depends_on: [db, redis]

    # Health check
    healthcheck:
      path: /health
      interval: 10s
      timeout: 5s
      retries: 3
      start_period: 30s

    # Lifecycle hooks
    hooks:
      post-up: "npm run migrate"
      pre-down: "npm run cleanup"

    # Environment
    env:
      NODE_ENV: production
      DATABASE_URL: postgres://db:5432/app
    env_file: .env
    secrets: [api_key]

    # Storage
    volumes:
      - data:/app/data
      - ./uploads:/app/uploads

    # Deployment
    deploy:
      cpu: 512
      memory: 1024
      desired_count: 2
      health_check_path: /health

  db:
    image: postgres:16-alpine
    port: 5432
    env:
      POSTGRES_PASSWORD: ${DB_PASSWORD:-secret}
    volumes:
      - pgdata:/var/lib/postgresql/data

# Named volumes
volumes:
  data:
  pgdata:

# Secrets
secrets:
  api_key:
    env: API_KEY           # From environment variable
  db_cert:
    file: ./certs/db.pem   # From file

# Registry for push/deploy
registry:
  type: ecr               # ecr, dockerhub
  region: us-west-2

# Cloud deployment
deploy:
  target: ecs
  ecs:
    cluster: my-cluster
    region: us-west-2

# Environment overrides
environments:
  staging:
    services:
      api:
        env:
          LOG_LEVEL: debug
        deploy:
          desired_count: 1

  production:
    services:
      api:
        env:
          LOG_LEVEL: warn
        deploy:
          desired_count: 3
          cpu: 1024
          memory: 2048
```

### Environment Variables

Use `${VAR}` or `${VAR:-default}` syntax:

```yaml
services:
  app:
    env:
      DATABASE_URL: ${DATABASE_URL}
      LOG_LEVEL: ${LOG_LEVEL:-info}
```

---

## Examples

### Node.js + PostgreSQL

```yaml
version: "1"
services:
  app:
    path: .
    port: 3000
    depends_on: [db]
    env:
      DATABASE_URL: postgres://postgres:secret@db:5432/app
  db:
    image: postgres:16-alpine
    port: 5432
    env:
      POSTGRES_PASSWORD: secret
```

### Python FastAPI + Redis

```yaml
version: "1"
services:
  api:
    path: .
    runtime: python
    port: 8000
    depends_on: [cache]
    env:
      REDIS_URL: redis://cache:6379
  cache:
    image: redis:7-alpine
    port: 6379
```

### Go Microservices

```yaml
version: "1"
services:
  gateway:
    path: ./gateway
    port: 8080
    depends_on: [users, orders]
  users:
    path: ./services/users
    port: 8081
  orders:
    path: ./services/orders
    port: 8082
```

---

## Tips & Tricks

### Use Environment-Specific Configs

```bash
cbox up --env staging
cbox deploy --env production
```

### Quick Database Access

```bash
cbox db shell db              # Open psql/mysql shell
cbox db snapshot db backup    # Create backup
cbox db restore db backup.sql # Restore backup
```

### Debugging

```bash
cbox diagnose          # Find common problems
cbox logs -f api       # Follow logs
cbox exec api -- sh    # Shell into container
cbox top api           # View processes
```

### CI/CD Integration

```bash
# Build with no output unless error
cbox build -q

# Wait for service to be healthy
cbox up -d && cbox wait api

# Run tests
cbox run api -- npm test
```

---

## Requirements

- **Docker** 23.0+ with BuildKit
- **Go** 1.21+ (for building from source)

---

## Contributing

Contributions are welcome! Please read our contributing guidelines before submitting PRs.

```bash
# Run tests
make test

# Run linter
make lint

# Build
make build
```

---

## License

MIT License - see [LICENSE](LICENSE) for details.

---

<p align="center">
  <sub>Built with simplicity in mind.</sub>
</p>
