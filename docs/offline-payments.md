# Offline Payments

## Overview

The Gonuts library provides comprehensive offline payment capabilities, allowing users to send ecash tokens without requiring active network connectivity. This document explains the offline functionality, current limitations, and implementation details.

## Current Offline Capabilities

### ✅ Supported Offline Operations

1. **Wallet Initialization**
   - Load existing wallet with cached keysets
   - Access stored proofs and balances
   - Use previously trusted mints

2. **Token Creation (Sending)**
   - Create tokens from existing proofs
   - Generate cashu tokens for transfer
   - Mark proofs as pending locally

3. **Balance Checking**
   - View current wallet balance
   - List proofs by mint
   - Check pending transactions

### ❌ Operations Requiring Network

1. **Token Validation (Receiving)**
   - Verify received tokens with mint
   - Swap tokens for new proofs
   - DLEQ proof verification

2. **New Mint Addition**
   - Fetch keyset from new mints
   - Validate mint information
   - Download public keys

3. **Keyset Updates**
   - Check for new active keysets
   - Update keyset metadata
   - Validate keyset changes

## Current Issues and Solutions

### Issue 1: Initialization Crashes When Offline

**Problem**: The wallet crashes during startup when offline due to network calls in [`loadWalletMints()`](../wallet/wallet.go:1808).

**Current Code**:
```go
// Line 1808 in wallet/wallet.go
if len(keyset.PublicKeys) == 0 {
    publicKeys, err := GetKeysetKeys(keyset.MintURL, keyset.Id)
    if err != nil {
        return nil, err  // Crashes here when offline
    }
    keyset.PublicKeys = publicKeys
    w.db.SaveKeyset(&keyset)
}
```

**Solution**: Add offline mode detection and graceful degradation:
```go
if len(keyset.PublicKeys) == 0 {
    if isOnline() {
        publicKeys, err := GetKeysetKeys(keyset.MintURL, keyset.Id)
        if err != nil {
            log.Printf("Failed to fetch keyset keys: %v", err)
            continue // Skip this keyset for now
        }
        keyset.PublicKeys = publicKeys
        w.db.SaveKeyset(&keyset)
    } else {
        log.Printf("Skipping keyset %s - offline mode", keyset.Id)
        continue
    }
}
```

### Issue 2: Send Operations Call Network

**Problem**: The [`Send()`](../wallet/wallet.go:411) function calls [`getActiveKeyset()`](../wallet/wallet.go:94) which makes network requests.

**Current Code**:
```go
// Line 94 in wallet/wallet.go
allKeysets, err := client.GetAllKeysets(mintURL)
if err != nil {
    return nil, err  // Fails when offline
}
```

**Solution**: Use cached keysets when offline:
```go
allKeysets, err := client.GetAllKeysets(mintURL)
if err != nil {
    if isNetworkError(err) {
        // Use cached keyset in offline mode
        log.Printf("Using cached keyset - offline mode")
        return &activeKeyset, nil
    }
    return nil, err
}
```

### Issue 3: Missing Offline Mode Management

**Problem**: No centralized offline mode detection or management.

**Solution**: Implement offline mode management:
```go
type OfflineManager struct {
    isOffline bool
    lastCheck time.Time
    checkInterval time.Duration
}

func (om *OfflineManager) IsOffline() bool {
    if time.Since(om.lastCheck) > om.checkInterval {
        om.isOffline = !om.checkConnectivity()
        om.lastCheck = time.Now()
    }
    return om.isOffline
}
```

## Implementation Plan

### Phase 1: Core Infrastructure

1. **Add Offline Mode Detection**
   ```go
   // wallet/connectivity.go
   func CheckConnectivity(mintURL string) bool {
       client := &http.Client{Timeout: 5 * time.Second}
       resp, err := client.Get(mintURL + "/v1/info")
       if err != nil {
           return false
       }
       defer resp.Body.Close()
       return resp.StatusCode == http.StatusOK
   }
   ```

2. **Enhance Keyset Storage**
   ```go
   // Add to crypto/keyset.go
   type WalletKeyset struct {
       // ... existing fields
       CachedAt    time.Time
       LastUsed    time.Time
       IsExpired   bool
   }
   ```

3. **Modify Wallet Initialization**
   ```go
   // wallet/offline.go
   func (w *Wallet) loadWalletMintsOffline() (map[string]walletMint, error) {
       // Load keysets from cache without network calls
       // Mark missing keysets for background refresh
   }
   ```

### Phase 2: Offline Send Operations

1. **Update Send Logic**
   ```go
   func (w *Wallet) Send(amount uint64, mintURL string, includeFees bool) (cashu.Proofs, error) {
       selectedMint, ok := w.mints[mintURL]
       if !ok {
           return nil, ErrMintNotExist
       }
       
       // Use cached keyset for offline operations
       if w.isOffline {
           return w.sendOffline(amount, &selectedMint, includeFees)
       }
       
       // Normal online send logic
       return w.sendOnline(amount, &selectedMint, includeFees)
   }
   ```

2. **Implement Offline Send**
   ```go
   func (w *Wallet) sendOffline(amount uint64, mint *walletMint, includeFees bool) (cashu.Proofs, error) {
       // Validate cached keyset is available
       if len(mint.activeKeyset.PublicKeys) == 0 {
           return nil, errors.New("no cached keyset available for offline operation")
       }
       
       // Use cached keyset for proof creation
       proofsToSend, err := w.getProofsForAmountOffline(amount, mint, includeFees)
       if err != nil {
           return nil, err
       }
       
       // Mark as pending without network validation
       if err := w.db.AddPendingProofs(proofsToSend); err != nil {
           return nil, fmt.Errorf("could not save proofs to pending: %v", err)
       }
       
       return proofsToSend, nil
   }
   ```

### Phase 3: Background Sync

1. **Implement Background Keyset Updates**
   ```go
   func (w *Wallet) StartBackgroundSync() {
       go func() {
           ticker := time.NewTicker(30 * time.Minute)
           defer ticker.Stop()
           
           for {
               select {
               case <-ticker.C:
                   if !w.isOffline {
                       w.syncKeysets()
                   }
               }
           }
       }()
   }
   ```

2. **Add Keyset Refresh Logic**
   ```go
   func (w *Wallet) syncKeysets() {
       for mintURL := range w.mints {
           if err := w.refreshMintKeysets(mintURL); err != nil {
               log.Printf("Failed to refresh keysets for %s: %v", mintURL, err)
           }
       }
   }
   ```

## Usage Examples

### Basic Offline Send

```go
package main

import (
    "fmt"
    "log"
    
    "github.com/elnosh/gonuts/cashu"
    "github.com/elnosh/gonuts/wallet"
)

func main() {
    // Initialize wallet
    config := wallet.Config{
        WalletPath:     "./wallet-data",
        CurrentMintURL: "https://mint.example.com",
    }
    
    w, err := wallet.LoadWallet(config)
    if err != nil {
        log.Fatal(err)
    }
    defer w.Shutdown()
    
    // Check if we can operate offline
    if w.IsOffline() {
        fmt.Println("Operating in offline mode")
    }
    
    // Send tokens (works offline if keysets are cached)
    mint := w.CurrentMint()
    amount := uint64(1000) // 1000 sats
    includeFees := true
    
    proofs, err := w.Send(amount, mint, includeFees)
    if err != nil {
        log.Fatal(err)
    }
    
    // Create token
    token, err := cashu.NewTokenV4(proofs, mint, cashu.Sat, false)
    if err != nil {
        log.Fatal(err)
    }
    
    tokenString, err := token.Serialize()
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Printf("Token: %s\n", tokenString)
}
```

### Offline Wallet Status

```go
func checkOfflineCapability(w *wallet.Wallet) {
    trustedMints := w.TrustedMints()
    fmt.Printf("Trusted mints: %d\n", len(trustedMints))
    
    for _, mintURL := range trustedMints {
        // Check if we have cached keysets for this mint
        if mint, ok := w.mints[mintURL]; ok {
            hasActiveKeyset := len(mint.activeKeyset.PublicKeys) > 0
            fmt.Printf("Mint %s: cached keyset available: %v\n", mintURL, hasActiveKeyset)
        }
    }
    
    balance := w.GetBalance()
    fmt.Printf("Total balance: %d sats\n", balance)
}
```

## Error Handling

### Network Error Detection

```go
func isNetworkError(err error) bool {
    if err == nil {
        return false
    }
    
    // Check for common network errors
    errStr := err.Error()
    networkErrors := []string{
        "no such host",
        "connection refused",
        "connection timeout",
        "network is unreachable",
        "temporary failure in name resolution",
    }
    
    for _, netErr := range networkErrors {
        if strings.Contains(errStr, netErr) {
            return true
        }
    }
    
    return false
}
```

### Graceful Degradation

```go
func (w *Wallet) getActiveKeysetWithFallback(mintURL string) (*crypto.WalletKeyset, error) {
    // Try network first
    keyset, err := w.getActiveKeyset(mintURL)
    if err == nil {
        return keyset, nil
    }
    
    // If network error, use cached keyset
    if isNetworkError(err) {
        if mint, ok := w.mints[mintURL]; ok {
            if len(mint.activeKeyset.PublicKeys) > 0 {
                log.Printf("Using cached keyset for %s", mintURL)
                return &mint.activeKeyset, nil
            }
        }
        return nil, fmt.Errorf("no cached keyset available for %s", mintURL)
    }
    
    return nil, err
}
```

## Best Practices

### 1. First-Time Setup
- Always perform initial wallet setup while online
- Ensure all required keysets are cached before going offline
- Verify sufficient balance for intended offline operations

### 2. Keyset Management
- Regular keyset updates when online
- Monitor keyset expiration
- Graceful handling of keyset rotation

### 3. Error Handling
- Distinguish between network and protocol errors
- Provide clear offline mode indicators
- Queue operations for later when appropriate

### 4. Security Considerations
- Validate cached keysets haven't been tampered with
- Implement proper keyset expiration
- Regular online verification of offline operations

## Limitations

### Current Limitations
1. **No offline receiving**: Tokens cannot be validated without mint contact
2. **No new mint support**: Cannot add new mints while offline
3. **No keyset updates**: Cannot update keysets without network
4. **Limited error recovery**: Some error states require online resolution

### Future Enhancements
1. **Optimistic receiving**: Accept tokens with delayed validation
2. **Peer-to-peer keyset sharing**: Share keysets between trusted wallets
3. **Offline proof verification**: Basic proof validation without mint contact
4. **Smart retry logic**: Intelligent network retry with exponential backoff

## Migration Guide

### Existing Wallets
Existing wallets will automatically gain offline capabilities after upgrading:

1. **Automatic keyset caching**: Keysets will be cached during normal operation
2. **Gradual offline support**: Offline capabilities will improve as keysets are cached
3. **No breaking changes**: Existing functionality remains unchanged

### New Wallets
New wallets should follow the offline-first setup:

1. **Initial online setup**: Perform first initialization online
2. **Keyset pre-caching**: Cache keysets for intended offline use
3. **Offline validation**: Test offline operations before relying on them

## Troubleshooting

### Common Issues

**"No cached keyset available"**
- Solution: Go online and perform a wallet operation to cache keysets

**"Mint not found"**
- Solution: Add mint while online, then use offline

**"Failed to create token"**
- Solution: Check balance and keyset availability

**"Network timeout"**
- Solution: Normal behavior in offline mode, operations should still work

### Debug Commands

```bash
# Check wallet status
nutw status --verbose

# List cached keysets
nutw keysets --cached

# Force keyset refresh
nutw keysets --refresh

# Test offline mode
nutw send --offline 1000