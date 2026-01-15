# cbox

A fast, opinionated container tool for local development. Less YAML, smarter defaults.

## Why cbox?

Docker Compose is powerful but verbose. cbox auto-detects your project and generates sensible defaults:

```bash
cd my-node-app
cbox init    # Detects Node.js, generates config
cbox up      # Builds and runs with one command
```

## Install

```bash
go install github.com/bobbyrathore/cbox/cmd/cbox@latest
```

Or build from source:
```bash
git clone https://github.com/bobbyrathore/cbox
cd cbox && go build -o bin/cbox ./cmd/cbox
```

## Quick Start

```bash
# Initialize in any project directory
cbox init

# Start services (detached)
cbox up -d

# View logs
cbox logs

# Run one-off commands
cbox run app -- npm run migrate

# Stop everything
cbox down
```

## Key Features

- **Zero-config mode** - Works without cbox.yaml for simple projects
- **Smart detection** - Auto-detects Node.js, Go, Python runtimes
- **Fast builds** - BuildKit caching out of the box
- **Port conflict resolution** - Finds alternative ports automatically
- **Config validation** - `cbox validate` catches errors before running
- **Dev mode** - `cbox dev` with hot reload support
- **Database tools** - `cbox db shell`, `cbox db snapshot`
- **Diagnostics** - `cbox diagnose` finds common problems

## Commands

| Command | Description |
|---------|-------------|
| `cbox init` | Generate cbox.yaml from project |
| `cbox up [-d]` | Start services |
| `cbox down` | Stop services |
| `cbox dev` | Development mode with hot reload |
| `cbox ps` | List running services |
| `cbox logs [service]` | View logs |
| `cbox exec <service> -- <cmd>` | Run command in container |
| `cbox run <service> -- <cmd>` | One-off command |
| `cbox restart [service]` | Restart services |
| `cbox validate` | Check config for errors |
| `cbox diagnose` | Find common problems |
| `cbox doctor` | Check system requirements |

## Example cbox.yaml

```yaml
version: "1"
project:
  name: my-app
services:
  app:
    path: .
    runtime: nodejs
    port: 3000
    command: ["npm", "start"]
    dev:
      command: ["npm", "run", "dev"]
      watch:
        paths: ["src/", "package.json"]
        ignore: ["node_modules/"]
      sync: true
  db:
    image: postgres:16-alpine
    port: 5432
    env:
      POSTGRES_PASSWORD: secret
```

## Requirements

- Docker with BuildKit (Docker 23.0+)
- Go 1.21+ (for building from source)

## License

MIT
