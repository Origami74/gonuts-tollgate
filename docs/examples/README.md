# Examples

This directory contains practical examples demonstrating how to use the Gonuts library for various Cashu operations.

## Available Examples

### Basic Examples
- [`basic-wallet.go`](./basic-wallet.go) - Simple wallet operations
- [`offline-wallet.go`](./offline-wallet.go) - Offline payment capabilities
- [`mint-server.go`](./mint-server.go) - Running a mint server

### Advanced Examples
- [`multi-mint-wallet.go`](./multi-mint-wallet.go) - Managing multiple mints
- [`p2pk-payments.go`](./p2pk-payments.go) - Pay-to-Public-Key operations
- [`htlc-payments.go`](./htlc-payments.go) - Hash Time Locked Contracts
- [`restore-wallet.go`](./restore-wallet.go) - Wallet restoration from seed

### Integration Examples
- [`lightning-integration.go`](./lightning-integration.go) - Lightning Network integration
- [`web-wallet.go`](./web-wallet.go) - Web-based wallet interface
- [`cli-tool.go`](./cli-tool.go) - Command-line tool implementation

## Running Examples

### Prerequisites

Make sure you have Go installed and the Gonuts library:

```bash
go get github.com/Origami74/gonuts-tollgate
```

### Basic Usage

```bash
# Run a basic example
go run basic-wallet.go

# Run with custom mint
MINT_URL=https://your-mint.com go run basic-wallet.go

# Run offline example
go run offline-wallet.go
```

### Example Structure

Each example follows this structure:

```go
package main

import (
    "fmt"
    "log"
    
    "github.com/Origami74/gonuts-tollgate/wallet"
    "github.com/Origami74/gonuts-tollgate/cashu"
)

func main() {
    // Setup
    config := wallet.Config{
        WalletPath:     "./example-wallet",
        CurrentMintURL: "https://testnut.cashu.space",
    }
    
    w, err := wallet.LoadWallet(config)
    if err != nil {
        log.Fatal(err)
    }
    defer w.Shutdown()
    
    // Example operations
    // ...
}
```

## Testing Examples

### Test Environment

For testing, use the test mint:

```bash
export MINT_URL=https://testnut.cashu.space
```

### Local Mint

To run examples with a local mint:

1. Start a local mint server:
```bash
cd ../cmd/mint
go run mint.go
```

2. Run examples with local mint:
```bash
export MINT_URL=http://localhost:3338
go run basic-wallet.go
```

## Example Categories

### 1. Basic Operations
Learn fundamental wallet operations like sending, receiving, and balance checking.

### 2. Offline Functionality
Understand how to use the wallet without network connectivity.

### 3. Advanced Features
Explore P2PK, HTLC, and other advanced Cashu features.

### 4. Integration Patterns
See how to integrate Gonuts into larger applications.

## Common Patterns

### Error Handling

```go
if err != nil {
    if isNetworkError(err) {
        log.Println("Network error - continuing offline")
        return
    }
    log.Fatal(err)
}
```

### Configuration

```go
config := wallet.Config{
    WalletPath:     getWalletPath(),
    CurrentMintURL: getMintURL(),
}
```

### Resource Cleanup

```go
defer func() {
    if err := wallet.Shutdown(); err != nil {
        log.Printf("Error shutting down wallet: %v", err)
    }
}()
```

## Best Practices

### 1. Always Close Resources
```go
defer wallet.Shutdown()
```

### 2. Handle Errors Gracefully
```go
if err != nil {
    log.Printf("Operation failed: %v", err)
    return
}
```

### 3. Use Environment Variables
```go
mintURL := os.Getenv("MINT_URL")
if mintURL == "" {
    mintURL = "https://testnut.cashu.space"
}
```

### 4. Validate Input
```go
if amount <= 0 {
    log.Fatal("Amount must be positive")
}
```

## Contributing Examples

To contribute new examples:

1. Create a new `.go` file in this directory
2. Follow the existing example structure
3. Add comprehensive comments
4. Include error handling
5. Test with both online and offline scenarios
6. Update this README with your example

### Example Template

```go
package main

// Example: [Brief description]
// This example demonstrates [detailed description]

import (
    "fmt"
    "log"
    
    "github.com/Origami74/gonuts-tollgate/wallet"
    "github.com/Origami74/gonuts-tollgate/cashu"
)

func main() {
    // Configuration
    config := wallet.Config{
        WalletPath:     "./example-wallet",
        CurrentMintURL: getenv("MINT_URL", "https://testnut.cashu.space"),
    }
    
    // Initialize wallet
    w, err := wallet.LoadWallet(config)
    if err != nil {
        log.Fatal(err)
    }
    defer w.Shutdown()
    
    // Example operations
    if err := exampleOperation(w); err != nil {
        log.Fatal(err)
    }
    
    fmt.Println("Example completed successfully!")
}

func exampleOperation(w *wallet.Wallet) error {
    // Implementation
    return nil
}

func getenv(key, defaultValue string) string {
    if value := os.Getenv(key); value != "" {
        return value
    }
    return defaultValue
}
```

## Debugging Examples

### Enable Debug Logging

```bash
DEBUG=true go run example.go
```

### Common Debug Commands

```bash
# Check wallet state
nutw balance --verbose

# List keysets
nutw keysets

# Show pending proofs
nutw pending

# Test connectivity
nutw info
```

## Troubleshooting

### Common Issues

**"Mint not accessible"**
- Check network connectivity
- Verify mint URL
- Try test mint: `https://testnut.cashu.space`

**"Insufficient balance"**
- Check balance: `nutw balance`
- Mint more tokens: `nutw mint 1000`

**"Invalid token"**
- Verify token format
- Check if token was already spent

**"Keyset not found"**
- Update keysets: `nutw keysets --refresh`
- Check mint status

## Additional Resources

- [Getting Started Guide](../getting-started.md)
- [Wallet Operations](../wallet-operations.md)
- [Offline Payments](../offline-payments.md)
- [API Reference](../api-reference.md)