# Feature Requests

Collected from developer testing feedback. Prioritized by demand and impact.

---

## High Priority

### 1. Auto-generate .dockerignore
**Status:** Planned for v0.5.6

`cbox init` should create a `.dockerignore` file to prevent bloated build contexts. Missing this caused 145MB transfers and 6x slower rebuilds.

Default ignores by runtime:
- **Node.js:** `node_modules/`, `.npm/`, `dist/`, `.next/`, `coverage/`
- **Python:** `__pycache__/`, `.venv/`, `venv/`, `*.pyc`, `.pytest_cache/`
- **Go:** `vendor/` (if not vendoring), `*.test`
- **All:** `.git/`, `.env`, `*.log`, `.DS_Store`

### 2. `cbox push` - Registry Push
Push built images to container registries.

```bash
cbox push api                                    # Push to default registry
cbox push api --registry=docker.io/myuser       # Docker Hub
cbox push api --registry=123456.dkr.ecr.us-east-1.amazonaws.com  # ECR
```

### 3. `cbox deploy` - Cloud Deployment
Deploy services to cloud platforms.

```bash
cbox deploy --target=ecs          # AWS ECS/Fargate
cbox deploy --target=cloudrun     # Google Cloud Run
cbox deploy --target=railway      # Railway.app
```

### 4. Multi-Runtime Support
Currently only Node.js is fully implemented. Need:
- **Python** (Flask, FastAPI, Django)
- **Go** (standard library, Gin, Echo)
- **Rust** (Actix, Axum)
- **Ruby** (Rails, Sinatra)
- **Java** (Spring Boot)

---

## Medium Priority

### 5. Multi-Environment Support
Override configs per environment.

```bash
cbox up --env staging     # Uses cbox.staging.yaml overrides
cbox up --env production  # Uses cbox.production.yaml overrides
```

### 6. Service Profiles
Group services for partial stack startup.

```yaml
profiles:
  full: [api, db, cache, worker, scheduler]
  minimal: [api, db]
  backend: [api, db, cache]
```

```bash
cbox up --profile minimal
```

### 7. `cbox env` - Environment Management
Better handling of environment variables and secrets.

```bash
cbox env list              # Show all env vars for services
cbox env set API_KEY=xxx   # Set env var
cbox env validate          # Check for missing required vars
```

### 8. Health Check Wait on depends_on
Currently `depends_on` only orders startup. Should wait for health checks.

```yaml
services:
  api:
    depends_on:
      db:
        condition: service_healthy  # Wait for DB health check
```

### 9. `cbox logs` Improvements
More filtering options for logs.

```bash
cbox logs --since 5m           # Last 5 minutes
cbox logs --until 10m          # Up to 10 minutes ago
cbox logs --grep "error"       # Filter by pattern
cbox logs --level error        # Filter by severity
```

---

## Nice to Have

### 10. VS Code Extension
- Syntax highlighting for `cbox.yaml`
- Service status in sidebar
- Click to start/stop services
- Log streaming in output panel
- Config validation on save

### 11. `cbox template` - Starter Templates
Pre-built configs for common stacks.

```bash
cbox init --template node-postgres
cbox init --template python-redis
cbox init --template fullstack    # Node + Postgres + Redis + nginx
```

### 12. `cbox scale` - Multi-Instance
Run multiple instances for load testing.

```bash
cbox scale api=3    # Run 3 instances of api service
```

### 13. Built-in Tunnel (ngrok/Cloudflare)
Expose local services publicly for testing.

```bash
cbox tunnel api    # Get public URL for api service
```

### 14. `cbox profile` - Build Analysis
Show build time breakdown, identify slow layers.

```bash
cbox profile build api
# Layer 1: FROM node:20-slim     0.1s (cached)
# Layer 2: COPY package*.json    0.2s (cached)
# Layer 3: RUN npm ci            45.2s  <-- SLOW
# Layer 4: COPY . .              0.3s
# Layer 5: RUN npm run build     12.1s
```

### 15. Plugin System
Allow extending cbox with custom commands.

```yaml
# cbox.yaml
plugins:
  - ./scripts/cbox-lint.sh
  - npm:cbox-plugin-typescript
```

---

## Completed

- [x] Container networking (v0.5.5) - Services resolve by hostname
- [x] Health check fixes (v0.5.5) - wget + timing defaults
- [x] Dev mode fixes (v0.5.5) - Docker validation + log streaming
- [x] Port conflict auto-resolution (v0.3.0)
- [x] Config validation (v0.5.1)
- [x] Lifecycle hooks (v0.5.2)
- [x] Smart diagnostics (v0.5.3)
- [x] Database snapshots (v0.4.0)
