# Efin Proxy

**Efin Proxy** is a lightweight, CLI-focused HTTP/HTTPS intercepting proxy written in Go, designed for simplicity and extensibility. It is tailored for users who prefer command-line applications, offering a straightforward way to inspect, modify, and log HTTP traffic. The proxy supports a powerful plugin system using gRPC, allowing developers to hook into the request and response lifecycle for custom processing.

Status: alpha

## Features

- **Simplicity**: Minimal setup with a focus on CLI usage, making it easy to integrate into workflows.
- **MITM Proxy**: Supports HTTPS interception with dynamic certificate generation or custom CA certificates.
- **gRPC Plugin System**: Extensible architecture to hook into HTTP request and response lifecycle using gRPC.
- **Scope Filtering**: Filter traffic by domain regex and exclude specific file extensions.
- **Logging and Storage**: Save requests/responses to SQLite database or files, with optional raw logging to stdout.
- **WebSocket Support**: Pass-through support for WebSocket connections.

## Installation

1. Ensure you have [Go](https://golang.org/) installed (version 1.24 or later).
2. Clone the repository:
```bash
git clone https://github.com/artilugio0/efin-proxy.git
cd efin-proxy
```
3. Build the proxy:
```bash
go build -o efin-proxy ./cmd/proxy
```

## Usage
Run the proxy with default settings:
```bash
./efin-proxy
```

The proxy listens on :8080 by default. Configure your browser or application to use localhost:8080 as the proxy.
## Command-Line Flags
The proxy supports the following flags to customize its behavior:
* `-cert <path>`: Path to the Root CA certificate file (PEM format). If not provided, a new CA certificate is generated and printed to stdout.
    Example: `-cert root-ca.crt`
* `-key <path>`: Path to the Root CA private key file (PEM format). Required if -cert is specified.
    Example: `-key root-ca.key`
* `-d <directory>`: Directory to save request and response files. If empty, file saving is disabled.
    Example: `-d ./logs`
* `-D <path>`: Path to the SQLite database file (short form). If empty, database saving is disabled. Overrides -db-file if both are set.
    Example: `-D proxy.db`
* `-db-file <path>`: Path to the SQLite database file (long form). If empty, database saving is disabled.
    Example: `-db-file proxy.db`
* `-p`: Enable raw request and response logging to stdout. Disabled by default.
    Example: `-p`
* `-s <regex>`: Regular expression to specify domains in scope. Only requests to matching domains are processed.
    Example: `-s "example\.com$"`
* `-e <excluded_extensions>`: Comma-separated list of file extensions to exclude from processing (e.g., images, videos). Default extensions include .png, .jpg, .mp4, etc.
    Example: `-e png,jpg,gif`

Example command with multiple flags:
```bash
./efin-proxy -cert root-ca.crt -key root-ca.key -d ./logs -D proxy.db -p -s "example\.com$" -e png,jpg
```

## gRPC Plugin System
Proxy-Vibes features a flexible plugin system using gRPC, allowing developers to hook into the HTTP request and response lifecycle. Plugins can inspect or modify traffic by connecting to the gRPC server running on localhost:50051. The system supports six distinct hooks:

* `RequestIn`: Triggered when a request is received by the proxy (read-only). Use this to inspect incoming requests.
    gRPC Method: RequestIn
    Stream: Server streaming
* `RequestMod`: Triggered to allow modification of the request before it is sent to the destination (read/write). Return a modified request.
    gRPC Method: RequestMod
    Stream: Bidirectional streaming
* `RequestOut`: Triggered after the request is finalized and sent to the destination (read-only). Use this for logging or analysis.
    gRPC Method: RequestOut
    Stream: Server streaming
* `ResponseIn`: Triggered when a response is received from the destination (read-only). Use this to inspect incoming responses.
    gRPC Method: ResponseIn
    Stream: Server streaming
* `ResponseMod`: Triggered to allow modification of the response before it is sent to the client (read/write). Return a modified response.
    gRPC Method: ResponseMod
    Stream: Bidirectional streaming
* `ResponseOut`: Triggered after the response is finalized and sent to the client (read-only). Use this for logging or analysis.
    gRPC Method: ResponseOut
    Stream: Server streaming

### Example gRPC Client
An example gRPC client is provided in ./cmd/grpcclient. It demonstrates how to connect to the proxy and handle all six hooks. To run the client:

```bash
go run ./cmd/grpc-client
```

The client logs request and response details and echoes modified requests/responses for the RequestMod and ResponseMod hooks. Modify the client code to implement custom logic.

### Writing a Custom Plugin
Generate the gRPC code from the proto file (./internal/grpc/proto/proxy.proto):

```bash
protoc --go_out=. --go-grpc_out=. ./internal/grpc/proto/proxy.proto
```

Implement a gRPC client that connects to localhost:50051 and registers for the desired hooks.
Use the provided methods (RequestIn, RequestMod, etc.) to process HTTP traffic.

See `./cmd/grpc-client/main.go` for a reference implementation.

## Database Schema
When using the `-D` or `-db-file` flag, requests and responses are saved to a SQLite database. The schema includes:

* `requests`: Stores request details (ID, method, URL, body, timestamp).
* `responses`: Stores response details (ID, status code, body, content length).
* `headers`: Stores headers for requests and responses (name, value).
* `cookies`: Stores cookies for requests and responses (name, value).

## File Saving
When using the `-d` flag, requests and responses are saved as raw HTTP text files in the specified directory. Files are named `request-<ID>.txt` and `response-<ID>.txt`, where `<ID>` is a unique UUID.

## Security Notes

The proxy generates a Root CA certificate if none is provided. Save and install this certificate in your browser or system trust store to avoid SSL warnings.
The default TLS configuration skips verification for testing (InsecureSkipVerify: true). For production, provide a trusted CA certificate and enable verification.
Ensure the CA private key (-key) is stored securely to prevent unauthorized access.
