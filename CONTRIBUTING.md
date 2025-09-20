# Contributing to Nomad Cloud Hypervisor Driver

We welcome contributions to the Nomad Cloud Hypervisor Driver! This document outlines the process for contributing to the project.

## Development Setup

### Prerequisites

- Go 1.19 or later
- Git
- Cloud Hypervisor v48.0.0 or later
- Linux kernel with KVM support

### Building from Source

```bash
# Clone the repository
git clone https://github.com/ccheshirecat/nomad-driver-ch.git
cd nomad-driver-ch

# Install dependencies
go mod download

# Run tests
go test ./...

# Build the plugin
go build -o nomad-driver-ch .

# Install the plugin
sudo cp nomad-driver-ch /opt/nomad/plugins/
```

## Code Style

- Follow Go conventions and use `gofmt`
- Maintain >80% test coverage
- Use descriptive commit messages
- Update documentation for user-facing changes

## Testing

### Unit Tests

```bash
go test ./...
```

### Integration Tests

```bash
# Requires Cloud Hypervisor installation
sudo go test -v ./virt/... -run Integration
```

## Submitting Changes

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Write tests for your changes
4. Ensure all tests pass (`go test ./...`)
5. Run linting (`golangci-lint run`)
6. Commit with clear messages
7. Push to your fork
8. Create a Pull Request

## Development Guidelines

1. **Code Style**: Follow Go conventions and use `gofmt`
2. **Testing**: Maintain >80% test coverage
3. **Documentation**: Update docs for user-facing changes
4. **Compatibility**: Maintain backward compatibility
5. **Security**: Never commit secrets or credentials

## Architecture

The driver consists of several key components:

- `virt/driver.go`: Main driver implementation
- `virt/config.go`: Configuration handling
- `cloudhypervisor/`: Cloud Hypervisor integration
- `chnet/`: Network management
- `internal/shared/`: Shared domain structures

## Getting Help

- Check the [troubleshooting guide](docs/TROUBLESHOOTING.md)
- Open an issue for bugs or feature requests
- Join our community discussions

## License

This project is licensed under the Mozilla Public License 2.0 - see the [LICENSE](LICENSE) file for details.