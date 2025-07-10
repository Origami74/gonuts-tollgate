# Wallet Operations

## Overview

This document provides comprehensive documentation for wallet operations in the Gonuts library, covering initialization, balance management, sending, receiving, and advanced features.

## Wallet Initialization

### Basic Configuration

```go
import "github.com/Origami74/gonuts-tollgate/wallet"

config := wallet.Config{
    WalletPath:     "./wallet-data",        // Local storage path
    CurrentMintURL: "https://mint.host.com", // Default mint URL
}

wallet, err := wallet.LoadWallet(config)
if err != nil {
    log.Fatal(err)
}
defer wallet.Shutdown()
```

### Configuration Options

```go
type Config struct {
    WalletPath     string  // Directory for wallet database
    CurrentMintURL string  // Default mint URL
}
```

**WalletPath**: Directory where wallet data is stored (BoltDB files)
**CurrentMintURL**: Default mint for operations when no specific mint is provided

### Initialization Process

When loading a wallet, the following occurs:

1. **Seed Generation/Loading**
   - Generate new BIP39 seed if first time
   - Load existing seed from storage
   - Derive master private key

2. **Keyset Loading**
   - Load cached keysets from database
   - Validate keyset integrity
   - Fetch missing public keys (if online)

3. **Mint Validation**
   - Verify default mint accessibility
   - Update active keysets if changed
   - Cache mint information

```go
func LoadWallet(config Config) (*Wallet, error) {
    // Initialize storage
    db, err := InitStorage(config.WalletPath)
    if err != nil {
        return nil, err
    }

    // Load or generate seed
    seed := db.GetSeed()
    if len(seed) == 0 {
        seed, err = generateNewSeed()
        if err != nil {
            return nil, err
        }
        db.SaveMnemonicSeed(mnemonic, seed)
    }

    // Derive master key
    masterKey, err := hdkeychain.NewMaster(seed, &chaincfg.MainNetParams)
    if err != nil {
        return nil, err
    }

    // Initialize wallet
    wallet := &Wallet{
        db:        db,
        unit:      cashu.Sat,
        masterKey: masterKey,
    }

    // Load trusted mints
    wallet.mints, err = wallet.loadWalletMints()
    if err != nil {
        return nil, err
    }

    // Add default mint if new
    if _, ok := wallet.mints[config.CurrentMintURL]; !ok {
        _, err = wallet.AddMint(config.CurrentMintURL)
        if err != nil {
            return nil, err
        }
    }

    return wallet, nil
}
```

## Balance Operations

### Check Total Balance

```go
balance := wallet.GetBalance()
fmt.Printf("Total balance: %d sats\n", balance)
```

### Balance by Mint

```go
balanceByMint := wallet.GetBalanceByMints()
for mintURL, balance := range balanceByMint {
    fmt.Printf("Mint %s: %d sats\n", mintURL, balance)
}
```

### Detailed Balance Information

```go
// Get all proofs
proofs := wallet.GetProofs()
fmt.Printf("Total proofs: %d\n", len(proofs))

// Group by denomination
denominations := make(map[uint64]int)
for _, proof := range proofs {
    denominations[proof.Amount]++
}

for amount, count := range denominations {
    fmt.Printf("Amount %d: %d proofs\n", amount, count)
}
```

## Sending Operations

### Basic Send

```go
mint := wallet.CurrentMint()
amount := uint64(1000) // 1000 sats
includeFees := true

proofsToSend, err := wallet.Send(amount, mint, includeFees)
if err != nil {
    log.Fatal(err)
}

// Create token
token, err := cashu.NewTokenV4(proofsToSend, mint, cashu.Sat, false)
if err != nil {
    log.Fatal(err)
}

tokenString, err := token.Serialize()
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Token: %s\n", tokenString)
```

### Send with Fee Calculation

```go
func sendWithFeeEstimate(w *wallet.Wallet, amount uint64, mintURL string) {
    // Estimate fees
    mint, ok := w.mints[mintURL]
    if !ok {
        log.Fatal("Mint not found")
    }
    
    // Calculate approximate fees
    proofsNeeded := selectProofsForAmount(amount, &mint)
    estimatedFees := feesForProofs(proofsNeeded, &mint)
    
    fmt.Printf("Sending %d sats with estimated fees: %d sats\n", amount, estimatedFees)
    
    // Send with fees included
    proofsToSend, err := w.Send(amount, mintURL, true)
    if err != nil {
        log.Fatal(err)
    }
    
    actualAmount := proofsToSend.Amount()
    fmt.Printf("Actual amount sent: %d sats\n", actualAmount)
}
```

### Send Process Details

The send operation follows these steps:

1. **Proof Selection**
   - Select proofs from inactive keysets first
   - Use active keyset proofs if needed
   - Calculate fees based on proof count

2. **Fee Calculation**
   ```go
   func feesForProofs(proofs cashu.Proofs, mint *walletMint) uint {
       var fees uint = 0
       for _, proof := range proofs {
           // Fee depends on keyset
           if proof.Id == mint.activeKeyset.Id {
               fees += mint.activeKeyset.InputFeePpk
           } else if inactiveKeyset, ok := mint.inactiveKeysets[proof.Id]; ok {
               fees += inactiveKeyset.InputFeePpk
           }
       }
       return fees
   }
   ```

3. **Proof Validation**
   - Verify proof ownership
   - Check keyset validity
   - Ensure sufficient balance

4. **Pending State**
   - Mark proofs as pending
   - Prevent double-spending
   - Store in database

## Receiving Operations

### Basic Receive

```go
tokenString := "cashuAeyJ0b2tlbiI6W3sibW..."
token, err := cashu.DecodeToken(tokenString)
if err != nil {
    log.Fatal(err)
}

swapToTrustedMint := true
amountReceived, err := wallet.Receive(token, swapToTrustedMint)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Received: %d sats\n", amountReceived)
```

### Receive Process

1. **Token Decoding**
   ```go
   func (w *Wallet) Receive(token cashu.Token, swapToTrusted bool) (uint64, error) {
       proofs := token.Proofs()
       tokenMint := token.Mint()
       
       // Verify token integrity
       if err := w.verifyToken(token); err != nil {
           return 0, err
       }
   ```

2. **Proof Verification**
   ```go
   // Get keyset for verification
   keyset, err := w.getActiveKeyset(tokenMint)
   if err != nil {
       return 0, err
   }
   
   // Verify DLEQ proofs if present
   if !nut12.VerifyProofsDLEQ(proofs, *keyset) {
       return 0, errors.New("invalid DLEQ proof")
   }
   ```

3. **Mint Trust Check**
   ```go
   // Check if mint is trusted
   _, trusted := w.mints[tokenMint]
   if !trusted {
       if swapToTrusted {
           // Swap to default mint
           return w.swapToTrustedMint(proofs, tokenMint)
       } else {
           // Add mint to trusted list
           _, err := w.AddMint(tokenMint)
           if err != nil {
               return 0, err
           }
       }
   }
   ```

4. **Proof Swap**
   ```go
   // Create swap request
   swapRequest, err := w.createSwapRequest(proofs, mint)
   if err != nil {
       return 0, err
   }
   
   // Perform swap
   newProofs, err := swap(tokenMint, swapRequest)
   if err != nil {
       return 0, err
   }
   
   // Store new proofs
   if err := w.db.SaveProofs(newProofs); err != nil {
       return 0, err
   }
   ```

## Advanced Send Operations

### Pay to Public Key (P2PK)

```go
import "github.com/btcsuite/btcd/btcec/v2"

// Generate recipient public key
recipientPrivKey, err := btcec.NewPrivateKey()
if err != nil {
    log.Fatal(err)
}
recipientPubKey := recipientPrivKey.PubKey()

// Create P2PK locked tokens
amount := uint64(1000)
mintURL := wallet.CurrentMint()
tags := &nut11.P2PKTags{
    Sigflag: nut11.SigAll,
}

lockedProofs, err := wallet.SendToPubkey(amount, mintURL, recipientPubKey, tags, true)
if err != nil {
    log.Fatal(err)
}

// Create token
token, err := cashu.NewTokenV4(lockedProofs, mintURL, cashu.Sat, false)
if err != nil {
    log.Fatal(err)
}

tokenString, err := token.Serialize()
fmt.Printf("P2PK Token: %s\n", tokenString)
```

### Hash Time Locked Contracts (HTLC)

```go
// Generate preimage
preimage := "secret_preimage_32_bytes_long_!!"
preimageHex := hex.EncodeToString([]byte(preimage))

// Create HTLC locked tokens
amount := uint64(1000)
mintURL := wallet.CurrentMint()
tags := &nut11.P2PKTags{
    Sigflag: nut11.SigAll,
}

lockedProofs, err := wallet.HTLCLockedProofs(amount, mintURL, preimageHex, tags, true)
if err != nil {
    log.Fatal(err)
}

// Create token
token, err := cashu.NewTokenV4(lockedProofs, mintURL, cashu.Sat, false)
if err != nil {
    log.Fatal(err)
}

tokenString, err := token.Serialize()
fmt.Printf("HTLC Token: %s\n", tokenString)
```

## Minting Operations

### Request Mint Quote

```go
amount := uint64(1000) // 1000 sats
mintURL := wallet.CurrentMint()

mintQuote, err := wallet.RequestMint(amount, mintURL)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Payment request: %s\n", mintQuote.PaymentRequest)
fmt.Printf("Quote ID: %s\n", mintQuote.Quote)
```

### Check Quote Status

```go
quoteState, err := wallet.MintQuoteState(mintQuote.Quote)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Quote state: %s\n", quoteState.State)
```

### Mint Tokens

```go
if quoteState.State == nut04.Paid {
    amountMinted, err := wallet.MintTokens(mintQuote.Quote)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Minted: %d sats\n", amountMinted)
}
```

## Melting Operations

### Request Melt Quote

```go
invoice := "lnbc1000n1pd..."
mintURL := wallet.CurrentMint()

meltQuote, err := wallet.RequestMeltQuote(invoice, mintURL)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Quote ID: %s\n", meltQuote.Quote)
fmt.Printf("Fee reserve: %d sats\n", meltQuote.FeeReserve)
```

### Melt Tokens

```go
meltResult, err := wallet.Melt(meltQuote.Quote)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Payment successful: %t\n", meltResult.Paid)
if len(meltResult.Preimage) > 0 {
    fmt.Printf("Preimage: %s\n", meltResult.Preimage)
}
```

## Mint Management

### Add Trusted Mint

```go
newMintURL := "https://new-mint.com"
mint, err := wallet.AddMint(newMintURL)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Added mint: %s\n", mint.mintURL)
```

### List Trusted Mints

```go
trustedMints := wallet.TrustedMints()
fmt.Printf("Trusted mints: %d\n", len(trustedMints))
for _, mintURL := range trustedMints {
    fmt.Printf("  - %s\n", mintURL)
}
```

### Update Mint URL

```go
oldURL := "https://old-mint.com"
newURL := "https://new-mint.com"

err := wallet.UpdateMintURL(oldURL, newURL)
if err != nil {
    log.Fatal(err)
}
```

## State Management

### Pending Proofs

```go
// Check pending proofs
pendingProofs := wallet.GetPendingProofs()
fmt.Printf("Pending proofs: %d\n", len(pendingProofs))

// Remove pending proofs (after confirmation)
proofSecrets := []string{"secret1", "secret2"}
err := wallet.RemovePendingProofs(proofSecrets)
if err != nil {
    log.Fatal(err)
}
```

### Proof History

```go
// Get all proofs
allProofs := wallet.GetProofs()

// Get proofs by keyset
activeKeyset := wallet.GetActiveKeyset(wallet.CurrentMint())
keysetProofs := wallet.GetProofsByKeysetId(activeKeyset.Id)

// Get proofs by mint
mintProofs := wallet.GetProofsByMint(wallet.CurrentMint())
```

## Restore Operations

### Restore from Seed

```go
// Restore wallet from mnemonic
mnemonic := "abandon abandon abandon ... abandon abandon art"
seed := bip39.NewSeed(mnemonic, "")

config := wallet.Config{
    WalletPath:     "./restored-wallet",
    CurrentMintURL: "https://mint.com",
}

// Initialize with existing seed
wallet, err := wallet.LoadWalletFromSeed(config, seed)
if err != nil {
    log.Fatal(err)
}

// Restore proofs from known mints
trustedMints := []string{
    "https://mint1.com",
    "https://mint2.com",
}

for _, mintURL := range trustedMints {
    restored, err := wallet.RestoreFromMint(mintURL)
    if err != nil {
        log.Printf("Failed to restore from %s: %v", mintURL, err)
        continue
    }
    fmt.Printf("Restored %d sats from %s\n", restored, mintURL)
}
```

### Restore Process

The restore operation attempts to recreate proofs by:

1. **Keyset Enumeration**
   - Fetch all keysets from mint
   - Try both active and inactive keysets

2. **Deterministic Secret Generation**
   - Use wallet's