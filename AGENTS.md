# Agent Guidelines for efin-proxy

## Build Commands
- **Build binary**: `go build -o efin-proxy ./cmd/efin-proxy`
- **Build script**: `./scripts/buid.sh`
- **Generate protobuf**: `./scripts/proto-gen.sh`

## Test Commands
- **All tests**: `./scripts/test-run.sh` or `go test ./...`
- **Single test**: `go test -run TestName ./path/to/package`
- **Coverage**: `go test -cover ./...`

## Code Style Guidelines

### Formatting
- Use standard Go formatting (`gofmt`)
- 4-space indentation (Go default)

### Imports
- Standard library imports first
- Third-party imports second
- Internal/project imports last
- Group with blank lines between categories

### Naming Conventions
- **Exported**: PascalCase (functions, types, constants)
- **Unexported**: camelCase (functions, variables)
- **Types**: Descriptive names, avoid abbreviations
- **Interfaces**: End with "er" (e.g., `IDProvider`, `InScopeFunc`)

### Error Handling
- Return errors as last return value
- Use `fmt.Errorf` for wrapping errors
- Handle errors immediately, don't ignore

### Types and Structs
- Use pointer receivers for methods that modify state
- Define interfaces for contracts
- Use embedded structs for composition

### Comments
- Godoc style for exported functions/types
- Comments explain why, not what
- No unnecessary comments

### Testing
- Table-driven tests preferred
- Test file naming: `*_test.go`
- Use `t.Fatalf` for setup failures, `t.Error` for assertion failures