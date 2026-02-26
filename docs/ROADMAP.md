# cbox Roadmap

## Design Principles
1. **Zero/minimal dependencies** - Pure Go, use Docker CLI
2. **Fully testable** - Every feature must be integration testable
3. **Solves real pain** - Focus on problems devs face daily
4. **Better than alternatives** - Don't just copy, innovate

---

## Competitive Analysis

| Tool | Strengths | Weaknesses |
|------|-----------|------------|
| Docker Compose | Industry standard | Verbose, no smart defaults, slow |
| Tilt | K8s dev, live update | Complex, K8s only |
| Skaffold | Google backed, K8s | K8s only, complex config |
| DevContainers | VS Code integration | Editor specific |

**cbox opportunity:** Fast, opinionated, works everywhere, solves the 80% case brilliantly.

---

## Proposed Features (Prioritized)

### Tier 1: Essential Gaps (v0.5.x)

#### 1. `cbox run <service> -- <command>`
Run one-off commands in service context.

```bash
cbox run api -- npm run migrate
cbox run api -- node scripts/seed.js
cbox run db -- psql -U postgres -c "SELECT 1"
```

**Why:** Every project needs migrations, seeds, scripts. Currently painful.

**Testability:**
- Test: Run `echo hello` in container, verify output
- Test: Run command that uses service's env vars
- Test: Run command that accesses service's network

---

#### 2. `cbox restart [service...]`
Restart without full down/up cycle.

```bash
cbox restart           # All services
cbox restart api       # One service
cbox restart api db    # Multiple
```

**Why:** Most common operation. Currently requires down+up.

**Testability:**
- Test: Container ID changes after restart
- Test: Volume data persists across restart
- Test: Env vars still correct after restart

---

#### 3. `cbox copy`
Copy files to/from containers.

```bash
cbox copy api:/app/logs ./local-logs
cbox copy ./config.json api:/app/config.json
```

**Why:** Essential for debugging, log extraction, config injection.

**Testability:**
- Test: Copy file from container, verify contents
- Test: Copy file to container, exec cat to verify
- Test: Copy directory recursively

---

#### 4. `cbox config show`
Show resolved configuration.

```bash
cbox config show
cbox config show --service api
cbox config show --format json
```

**Why:** Debug config issues. "What env vars will actually be set?"

**Testability:**
- Test: Shows correct values after env substitution
- Test: Shows secrets resolved
- Test: JSON output is valid JSON

---

#### 5. `cbox validate`
Validate cbox.yaml without running anything.

```bash
cbox validate
# ✓ Configuration valid
# ⚠ Warning: Service 'api' exposes port 3000 but no healthcheck defined
# ✗ Error: Service 'worker' depends on undefined service 'cache'
```

**Why:** Catch errors early. CI integration.

**Testability:**
- Test: Valid config passes
- Test: Invalid YAML fails with clear error
- Test: Circular dependency detected
- Test: Missing dependency detected
- Test: Returns exit code 0/1 appropriately

---

### Tier 2: Differentiators (v0.6.x)

#### 6. `cbox wait`
Wait for services to be healthy.

```bash
cbox wait                    # Wait for all
cbox wait api db             # Wait for specific
cbox wait --timeout 60s      # Custom timeout
```

**Why:** Scripts/CI need to wait. Compose depends_on doesn't wait for healthy.

**Testability:**
- Test: Exits 0 when service becomes healthy
- Test: Exits 1 on timeout
- Test: Works with multiple services
- Test: Respects timeout flag

---

#### 7. `cbox hook` - Lifecycle Hooks
Run commands at lifecycle points.

```yaml
services:
  api:
    hooks:
      post-up: npm run db:migrate
      pre-down: npm run cleanup
```

```bash
cbox up
# Running post-up hook for api...
# ✓ Hook completed
```

**Why:** Everyone needs migrations on start. Currently requires wrapper scripts.

**Testability:**
- Test: post-up hook runs after container healthy
- Test: pre-down hook runs before stop
- Test: Hook failure stops the operation
- Test: Hook has access to service network

---

#### 8. `cbox compose export`
Export to docker-compose.yml.

```bash
cbox compose export > docker-compose.yml
cbox compose export --env prod > docker-compose.prod.yml
```

**Why:** Interop with existing tools. Team members without cbox.

**Testability:**
- Test: Output is valid docker-compose YAML
- Test: Can run `docker compose up` on output
- Test: Services, ports, volumes correctly translated

---

#### 9. `cbox deps`
Visualize and manage dependencies.

```bash
cbox deps
# db
# └─► api (depends_on: db)
#     └─► worker (depends_on: api)

cbox deps check
# ✓ All dependencies satisfied
# ✗ Circular dependency: api → worker → api
```

**Why:** Understand complex projects. Debug dependency issues.

**Testability:**
- Test: Correct graph for linear dependencies
- Test: Correct graph for diamond dependencies
- Test: Detects circular dependencies
- Test: `deps check` returns correct exit code

---

#### 10. `cbox add <template>`
Add pre-configured services.

```bash
cbox add postgres
# Added service 'postgres' to cbox.yaml
# - Image: postgres:16-alpine
# - Port: 5432
# - Volume: postgres_data
# - Healthcheck: pg_isready

cbox add redis
cbox add mongo
cbox add mailhog
```

**Why:** Setting up postgres "correctly" takes googling. This makes it instant.

**Testability:**
- Test: Adds valid service to cbox.yaml
- Test: `cbox up` works after add
- Test: Healthcheck works for added service
- Test: Multiple adds don't conflict

---

### Tier 3: Power Features (v0.7.x)

#### 11. `cbox diagnose`
Smart problem detection.

```bash
cbox diagnose
# Checking myproject...
#
# Issues found:
#   ✗ Service 'api' is crash-looping (3 restarts in 1 minute)
#     → Last log: "Error: connect ECONNREFUSED 172.18.0.2:5432"
#     → Suggestion: Database not ready. Add healthcheck to 'db' service.
#
#   ⚠ Port 3000 remapped to 3001 (conflict with PID 1234)
#
#   ⚠ Service 'worker' using 89% memory
```

**Why:** Debugging containers is painful. This automates the checklist.

**Testability:**
- Test: Detects container crash loops
- Test: Detects port remapping
- Test: Detects high memory usage
- Test: Detects network connectivity issues
- Test: Returns structured output (JSON flag)

---

#### 12. `cbox proxy`
Inspect traffic between services.

```bash
cbox proxy start
# Proxy started. Dashboard: http://localhost:19999

cbox proxy requests
# TIME       FROM    TO      METHOD  PATH         STATUS  DURATION
# 10:23:45   api     db      TCP     :5432        OK      23ms
# 10:23:46   api     redis   TCP     :6379        OK      2ms
```

**Why:** Debugging microservice communication is hell.

**Testability:**
- Test: Captures requests between services
- Test: Shows correct timing
- Test: Filters by service
- Test: JSON output for scripting

---

#### 13. `cbox test`
Run tests with fresh containers.

```bash
cbox test
# Starting fresh containers...
# Waiting for healthy...
# Running: npm test
# ... test output ...
# Cleaning up...
# ✓ Tests passed

cbox test --keep    # Keep containers for debugging
cbox test api       # Test specific service
```

**Why:** Everyone writes this in CI scripts. Built-in is better.

**Testability:**
- Test: Containers are fresh (not reused)
- Test: Exit code matches test exit code
- Test: --keep flag preserves containers
- Test: Cleanup happens even on test failure

---

## Implementation Priority

Based on: Value + Testability + Effort

| Version | Features | Notes |
|---------|----------|-------|
| v0.5.0 | run, restart, copy | Essential gaps, easy to test |
| v0.5.1 | config show, validate | Config tooling, easy to test |
| v0.6.0 | wait, hooks | Lifecycle management |
| v0.6.1 | compose export | Interoperability |
| v0.6.2 | deps, add | DX improvements |
| v0.7.0 | diagnose | Smart tooling |
| v0.7.1 | test | Testing integration |
| v0.8.0 | proxy | Advanced debugging |

---

## Non-Goals (For Now)

- Kubernetes support (stay focused on local dev)
- Cloud deployment beyond AWS (focused on AWS ECS/Fargate for now)
- GUI application (TUI is enough)
- Plugin system (keep it simple)
