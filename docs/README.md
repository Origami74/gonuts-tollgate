# Gonuts Library Documentation

This directory contains comprehensive documentation for the Gonuts cashu library, a Go implementation of the Cashu protocol for Bitcoin ecash.

## Table of Contents

- [Library Overview](./library-overview.md) - High-level architecture and components
- [Getting Started](./getting-started.md) - Installation and basic usage
- [Wallet Operations](./wallet-operations.md) - Detailed wallet functionality
- [Mint Operations](./mint-operations.md) - Mint server implementation
- [Offline Payments](./offline-payments.md) - Offline payment capabilities and limitations
- [Keyset Management](./keyset-management.md) - How keysets are managed and cached
- [Network Communication](./network-communication.md) - Client-server communication protocols
- [Storage Layer](./storage-layer.md) - Data persistence and database operations
- [Cryptographic Operations](./cryptographic-operations.md) - Cryptographic primitives and security
- [NUT Specifications](./nut-specifications.md) - Implemented Cashu NUT specifications
- [Error Handling](./error-handling.md) - Error types and handling strategies
- [Migration Guide](./migration-guide.md) - Upgrading between versions
- [API Reference](./api-reference.md) - Complete API documentation
- [Examples](./examples/) - Code examples and tutorials

## Quick Start

```go
import "github.com/Origami74/gonuts-tollgate/wallet"

// Initialize wallet
config := wallet.Config{
    WalletPath:     "./wallet-data",
    CurrentMintURL: "https://mint.example.com",
}

wallet, err := wallet.LoadWallet(config)
if err != nil {
    log.Fatal(err)
}
defer wallet.Shutdown()

// Check balance
balance := wallet.GetBalance()
fmt.Printf("Balance: %d sats\n", balance)
```

## Architecture Overview

The Gonuts library is structured around several key components:

- **Wallet**: Client-side operations for managing ecash
- **Mint**: Server-side operations for issuing and redeeming ecash
- **Cashu**: Core protocol types and utilities
- **Crypto**: Cryptographic operations and keyset management
- **Storage**: Data persistence layer with BoltDB implementation

## Key Features

- ✅ Full Cashu protocol implementation
- ✅ Deterministic wallet generation from seed
- ✅ Multi-mint support
- ✅ Lightning Network integration
- ✅ P2PK (Pay to Public Key) support
- ✅ HTLC (Hash Time Locked Contracts) support
- ✅ Offline payment capabilities
- ✅ Robust error handling and recovery

## Offline Payment Support

This library includes comprehensive offline payment capabilities:

- **Offline Initialization**: Wallet can start without network connectivity using cached keysets
- **Offline Token Creation**: Send tokens using locally stored proofs without mint contact
- **Graceful Degradation**: Automatic fallback to cached data when network is unavailable
- **Background Sync**: Automatic keyset updates when network becomes available

See [Offline Payments](./offline-payments.md) for detailed information.

## Security Considerations

- This library is experimental and not production-ready
- Always use test amounts for development and testing
- Backup your wallet seed phrase securely
- Verify mint trustworthiness before use
- Keep keysets updated for optimal security

## Contributing

See the main repository for contribution guidelines and development setup instructions.