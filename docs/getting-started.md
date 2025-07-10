# Getting Started

## Installation

### Prerequisites

- [Go](https://go.dev/doc/install) 1.19 or later
- Git

### Install from Source

```bash
# Clone the repository
git clone https://github.com/Origami74/gonuts-tollgate
cd gonuts

# Install the wallet CLI
go install ./cmd/nutw/

# Install the mint CLI (optional)
go install ./cmd/mint/
```

### Install as a Library

```bash
# Add to your Go module
go get github.com/Origami74/gonuts-tollgate
```

## Quick Start

### 1. Basic Wallet Setup

Create a simple wallet program:

```go
package main

import (
    "fmt"
    "log"
    
    "github.com/Origami74/gonuts-tollgate/wallet"
)

func main() {
    // Configure wallet
    config := wallet.Config{
        WalletPath:     "./my-wallet",
        CurrentMintURL: "https://testnut.cashu.space", // Test mint
    }
    
    // Load wallet (creates new one if doesn't exist)
    w, err := wallet.LoadWallet(config)
    if err != nil {
        log.Fatal(err)
    }
    defer w.Shutdown()
    
    // Check balance
    balance := w.GetBalance()
    fmt.Printf("Balance: %d sats\n", balance)
    
    // Get mnemonic for backup
    mnemonic := w.Mnemonic()
    fmt.Printf("Backup this mnemonic: %s\n", mnemonic)
}
```

### 2. Using the CLI

The `nutw` command provides a full-featured wallet CLI:

```bash
# Check balance
nutw balance

# Create lightning invoice to receive ecash
nutw mint 1000

# Pay the invoice, then mint the tokens
nutw mint --invoice lnbc1000n1pd...

# Send tokens
nutw send 500

# Receive tokens
nutw receive cashuAeyJ0b2tlbiI6W3...

# Pay lightning invoice
nutw pay lnbc500n1pd...
```

### 3. Configuration

Create a `.env` file in your wallet directory (`~/.gonuts/wallet/.env`):

```env
MINT_URL=https://testnut.cashu.space
WALLET_PATH=/path/to/wallet/data
```

Or set environment variables:

```bash
export MINT_URL=https://testnut.cashu.space
export WALLET_PATH=./wallet-data
```

## Basic Operations

### Receiving Ecash

1. **Request a mint quote**:
```go
amount := uint64(1000) // 1000 sats
mintURL := wallet.CurrentMint()

mintQuote, err := wallet.RequestMint(amount, mintURL)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Pay this invoice: %s\n", mintQuote.PaymentRequest)
```

2. **Check payment status**:
```go
quoteState, err := wallet.MintQuoteState(mintQuote.Quote)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Status: %s\n", quoteState.State)
```

3. **Mint tokens after payment**:
```go
if quoteState.State == nut04.Paid {
    amountMinted, err := wallet.MintTokens(mintQuote.Quote)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Minted: %d sats\n", amountMinted)
}
```

### Sending Ecash

```go
import "github.com/Origami74/gonuts-tollgate/cashu"

// Create tokens to send
amount := uint64(500)
mintURL := wallet.CurrentMint()
includeFees := true

proofsToSend, err := wallet.Send(amount, mintURL, includeFees)
if err != nil {
    log.Fatal(err)
}

// Create token string
token, err := cashu.NewTokenV4(proofsToSend, mintURL, cashu.Sat, false)
if err != nil {
    log.Fatal(err)
}

tokenString, err := token.Serialize()
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Send this token: %s\n", tokenString)
```

### Receiving Ecash

```go
// Receive token string
tokenString := "cashuAeyJ0b2tlbiI6W3..."

token, err := cashu.DecodeToken(tokenString)
if err != nil {
    log.Fatal(err)
}

// Receive tokens (swap to trusted mint)
swapToTrusted := true
amountReceived, err := wallet.Receive(token, swapToTrusted)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Received: %d sats\n", amountReceived)
```

### Spending Ecash (Lightning)

```go
// Request melt quote
invoice := "lnbc500n1pd..."
mintURL := wallet.CurrentMint()

meltQuote, err := wallet.RequestMeltQuote(invoice, mintURL)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Total cost: %d sats (including %d sats fee)\n", 
    meltQuote.Amount + meltQuote.FeeReserve, meltQuote.FeeReserve)

// Pay invoice
meltResult, err := wallet.Melt(meltQuote.Quote)
if err != nil {
    log.Fatal(err)
}

if meltResult.Paid {
    fmt.Printf("Payment successful!\n")
    if len(meltResult.Preimage) > 0 {
        fmt.Printf("Preimage: %s\n", meltResult.Preimage)
    }
}
```

## Complete Example

Here's a complete example demonstrating the full ecash flow:

```go
package main

import (
    "fmt"
    "log"
    "time"
    
    "github.com/Origami74/gonuts-tollgate/cashu"
    "github.com/Origami74/gonuts-tollgate/cashu/nuts/nut04"
    "github.com/Origami74/gonuts-tollgate/wallet"
)

func main() {
    // Initialize wallet
    config := wallet.Config{
        WalletPath:     "./example-wallet",
        CurrentMintURL: "https://testnut.cashu.space",
    }
    
    w, err := wallet.LoadWallet(config)
    if err != nil {
        log.Fatal(err)
    }
    defer w.Shutdown()
    
    fmt.Printf("Initial balance: %d sats\n", w.GetBalance())
    
    // Step 1: Request mint quote
    amount := uint64(1000)
    mintQuote, err := w.RequestMint(amount, w.CurrentMint())
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Printf("Pay this Lightning invoice: %s\n", mintQuote.PaymentRequest)
    fmt.Printf("Quote ID: %s\n", mintQuote.Quote)
    
    // Step 2: Wait for payment and mint tokens
    fmt.Println("Waiting for payment...")
    for {
        quoteState, err := w.MintQuoteState(mintQuote.Quote)
        if err != nil {
            log.Fatal(err)
        }
        
        if quoteState.State == nut04.Paid {
            fmt.Println("Payment received! Minting tokens...")
            mintedAmount, err := w.MintTokens(mintQuote.Quote)
            if err != nil {
                log.Fatal(err)
            }
            fmt.Printf("Minted: %d sats\n", mintedAmount)
            break
        }
        
        time.Sleep(2 * time.Second)
    }
    
    fmt.Printf("New balance: %d sats\n", w.GetBalance())
    
    // Step 3: Send some tokens
    sendAmount := uint64(300)
    proofsToSend, err := w.Send(sendAmount, w.CurrentMint(), true)
    if err != nil {
        log.Fatal(err)
    }
    
    token, err := cashu.NewTokenV4(proofsToSend, w.CurrentMint(), cashu.Sat, false)
    if err != nil {
        log.Fatal(err)
    }
    
    tokenString, err := token.Serialize()
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Printf("Created token: %s\n", tokenString)
    fmt.Printf("Balance after send: %d sats\n", w.GetBalance())
    
    // Step 4: Receive the tokens back (simulating another wallet)
    receivedAmount, err := w.Receive(token, true)
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Printf("Received: %d sats\n", receivedAmount)
    fmt.Printf("Final balance: %d sats\n", w.GetBalance())
}
```

## Environment Setup

### Development Environment

For development and testing, you can run a local mint:

```bash
# Clone the repository
git clone https://github.com/Origami74/gonuts-tollgate
cd gonuts

# Set up environment
cp .env.mint.example .env
# Edit .env with your Lightning node details

# Run the mint
cd cmd/mint
go run mint.go
```

### Production Environment

For production use:

1. **Use a trusted mint** - Research and verify the mint's reputation
2. **Secure your seed** - Back up your mnemonic phrase securely
3. **Start with small amounts** - Test with minimal funds first
4. **Monitor mint status** - Keep track of mint availability and policies

## Configuration Options

### Wallet Configuration

```go
type Config struct {
    WalletPath     string  // Local storage directory
    CurrentMintURL string  // Default mint URL
}
```

### Environment Variables

- `MINT_URL`: Default mint URL
- `WALLET_PATH`: Wallet data directory

### CLI Configuration

Create `~/.gonuts/wallet/.env`:

```env
# Mint configuration
MINT_URL=https://your-mint.com

# Wallet storage
WALLET_PATH=/path/to/wallet/data

# Optional: Enable debug logging
DEBUG=true
```

## Error Handling

### Common Errors

1. **Network Connectivity**
```go
_, err := wallet.LoadWallet(config)
if err != nil {
    if isNetworkError(err) {
        log.Println("Network error - running in offline mode")
        // Handle offline mode
    } else {
        log.Fatal(err)
    }
}
```

2. **Insufficient Balance**
```go
_, err := wallet.Send(amount, mintURL, true)
if err != nil {
    if errors.Is(err, wallet.ErrInsufficientMintBalance) {
        log.Printf("Insufficient balance. Need %d sats", amount)
    } else {
        log.Fatal(err)
    }
}
```

3. **Invalid Tokens**
```go
_, err := wallet.Receive(token, true)
if err != nil {
    log.Printf("Failed to receive token: %v", err)
    // Handle invalid token
}
```

## Best Practices

### Security

1. **Backup your seed phrase** - Store it securely offline
2. **Use trusted mints** - Research mint operators
3. **Start with small amounts** - Test with minimal funds
4. **Regular backups** - Keep wallet data backed up

### Performance

1. **Batch operations** - Group multiple operations when possible
2. **Monitor keyset health** - Keep keysets updated
3. **Manage proof sizes** - Avoid excessive small proofs

### Development

1. **Use test mints** - Don't use real money during development
2. **Handle errors gracefully** - Implement proper error handling
3. **Test offline scenarios** - Verify offline functionality
4. **Monitor logs** - Enable debug logging during development

## Next Steps

- Read the [Wallet Operations](./wallet-operations.md) guide for detailed operations
- Learn about [Offline Payments](./offline-payments.md) for offline usage
- Explore [Keyset Management](./keyset-management.md) for advanced keyset handling
- Check out the [API Reference](./api-reference.md) for complete documentation

## Troubleshooting

### Common Issues

**Wallet won't start**
- Check network connectivity
- Verify mint URL is accessible
- Ensure wallet directory is writable

**Can't receive tokens**
- Verify token format
- Check mint trustworthiness
- Ensure sufficient network connectivity

**Send operations fail**
- Check balance
- Verify mint is online
- Ensure keysets are up to date

### Debug Mode

Enable debug logging:

```go
import "log"

// Enable debug logging
log.SetFlags(log.LstdFlags | log.Lshortfile)
```

Or with CLI:

```bash
DEBUG=true nutw balance
```

### Getting Help

- Check the [documentation](./README.md)
- Review [examples](../examples/)
- Report issues on GitHub
- Join the Cashu community discussions