# cbox - Implemented Features

## v0.1.0 - Core Foundation
- `cbox init` - Auto-detect project type, generate cbox.yaml
- `cbox build` - Build container images with BuildKit
- `cbox up` - Start services with dependency ordering
- `cbox down` - Stop and remove containers
- `cbox ps` - List running services
- `cbox logs` - Stream service logs
- `cbox exec` - Execute commands in containers
- `cbox dev` - Development mode with hot reload
- `cbox doctor` - Check Docker/BuildKit availability
- Zero-config mode (works without cbox.yaml)
- Smart Dockerfile generation for Node.js

## v0.2.0 - Configuration & Secrets
- Environment variable substitution (`${VAR}`, `${VAR:-default}`)
- `.env` file loading (`env_file:` directive)
- Secrets management (from env vars or files)
- E2E test suite

## v0.3.0 - Smart Defaults & Port Resolution
- Smart default command (`cbox` with no args auto-detects action)
- Auto port resolution (finds alternative when port in use)

## v0.3.1 - Resource Monitoring
- `cbox top` - Live CPU/memory/network monitoring
- `cbox clean` - Remove stopped containers, dangling images

## v0.3.2 - TUI Dashboard
- `cbox dashboard` - Interactive terminal UI
- Vim-style navigation
- Start/stop/restart from TUI

## v0.4.0 - Database Tools
- `cbox db shell` - Interactive database shells (postgres, mysql, mongo, redis)
- `cbox db snapshot create/list/restore/delete` - Database snapshots

## v0.4.1 - SSH Tunnel
- `cbox tunnel` - SSH reverse tunnel to expose local services
- Multiple port mappings
- SSH agent and key file authentication

## v0.5.0 - Run & Restart Commands
- `cbox run <service> -- <command>` - Run one-off commands in service context
- `cbox restart [service...]` - Restart services without full down/up cycle

## v0.5.1 - Wait & Validate Commands
- `cbox wait [service...]` - Wait for services to be healthy (CI/scripting)
- `cbox validate` - Validate cbox.yaml configuration without running
  - Checks YAML syntax, dependencies, circular deps, port conflicts
  - `--strict` flag treats warnings as errors

## v0.5.2 - Lifecycle Hooks
- `hooks.post-up` - Run commands after container is healthy
- `hooks.pre-down` - Run commands before container stops
- Hook failure on post-up stops the up process
- Hook failure on pre-down only warns (doesn't block shutdown)

## v0.5.3 - Smart Diagnostics
- `cbox diagnose` - Smart problem detection
  - Crash loop detection (restart count)
  - Health check failures
  - High memory usage (>80% of limit)
  - Port conflicts/remapping
  - Missing dependencies
  - Connection refused errors in logs
- `--json` flag for scripting
