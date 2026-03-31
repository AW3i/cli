# valet-sh CLI

Go-based CLI that orchestrates Ansible playbooks for managing local development environments for Magento, PHP, and other projects.

## Architecture

### Entry Point
`cmd/valet/main.go` - Cobra-based CLI with 17 subcommands

### Package Structure
```
cli/
├── cmd/valet/           # Entry point (main.go)
├── internal/
│   ├── ansible/         # Subprocess runner for ansible-playbook
│   ├── commands/        # Cobra command implementations
│   ├── config/          # .valet-sh.yml and global config parsers
│   ├── platform/        # OS/arch detection, service name normalization
│   └── updater/         # Weekly update check from GitHub releases
└── .golangci.yml        # Linting configuration
```

### Key Design Decisions

1. **Ansible Orchestration**: The Go CLI doesn't replace Ansible - it wraps it. All commands eventually call `ansible.Run()` which execs into `ansible-playbook`.

2. **Process Replacement**: Uses `syscall.Exec()` to replace the Go process with ansible-playbook. This ensures signals (Ctrl-C) flow directly to Ansible.

3. **No Nolint Comments**: We use `.golangci.yml` exclusions for intentional blank assignments rather than scattering `//nolint` throughout code.

4. **Error Handling Strategy**:
   - Critical paths: Return errors
   - Help output: Blank assignments (stdout errors are non-critical)
   - Best-effort ops: Blank assignments (file close, HTTP body close)

## Development

### Prerequisites
- Go 1.22+
- golangci-lint v1.64.8 (auto-installed via Makefile)

### Build
```bash
cd cli
make build          # Build for current platform
make build-all      # Cross-compile for all platforms
make install        # Install to /usr/local/valet-sh/bin/
```

### Test
```bash
make test           # Run tests with race detector
make test-coverage  # Run with coverage report
```

### Lint
```bash
make lint           # Run golangci-lint (auto-installs if needed)
make lint-ci        # Run exactly as CI does
make quality        # Run all quality checks (fmt, vet, mod-verify, lint)
```

### CI/CD
Quality checks run on:
- Push to: main, master, next, 2.x
- Pull requests to above branches
- Only when cli/** files change

## Configuration

### .valet-sh.yml Format
```yaml
hub:
  host: "git.example.com"
  port: 22
  path: "/data"
services:
  php:
    version: 8.1
  mariadb:
    version: 10.6
    database: magento
instance:
  key: "myproject"
  type: "magento2"
```

### golangci-lint Configuration
See `.golangci.yml` for enabled linters. Key exclusions:
- `errcheck` disabled for: help.go, helpers.go, updater/check.go (intentional blank assignments)
- `G204` (syscall.Exec) excluded - intentional process replacement

## Commands

All 17 commands follow the same pattern:
1. Parse CLI args with Cobra
2. Read .valet-sh.yml if needed (with validation)
3. Build extra-vars JSON for Ansible
4. Call `ansible.Run()` which execs ansible-playbook

## Release Process

1. Tag with `v*` pattern: `git tag v2.10.0`
2. GitHub Actions builds 4 binaries:
   - valet-linux-amd64
   - valet-linux-arm64
   - valet-darwin-amd64
   - valet-darwin-arm64
3. Binaries + checksums.txt attached to GitHub Release
4. Installer downloads appropriate binary

## Common Issues

### Linter Errors
DO NOT add `//nolint` comments. Instead:
1. Fix the issue properly, OR
2. Add exclusion to `.golangci.yml` if intentional

### Ansible Not Found
The CLI looks for ansible-playbook at:
1. `/usr/local/valet-sh/venv/bin/ansible-playbook` (preferred)
2. `/usr/local/bin/ansible-playbook`
3. `$PATH`

## Security Notes

- `syscall.Exec()` is intentional and documented in code
- argv/env constructed from trusted sources (platform package, CLI args)
- Same behavior as original bash wrapper

## License

Apache 2.0 - see repository root for full license text.
