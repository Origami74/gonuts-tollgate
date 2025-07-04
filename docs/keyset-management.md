# Keyset Management

## Overview

Keysets are fundamental to the Cashu protocol, containing the public keys used to verify blinded signatures from mints. This document explains how the Gonuts library manages keysets, including caching, rotation, and offline support.

## Keyset Fundamentals

### What is a Keyset?

A keyset is a collection of public/private key pairs used by a mint to sign blinded messages. Each keyset:

- Contains keys for specific denominations (powers of 2)
- Has a unique identifier derived from the public keys
- Is associated with a specific mint and unit (e.g., "sat")
- Can be active or inactive

### Keyset Structure

```go
type WalletKeyset struct {
    Id          string                              // Unique keyset identifier
    MintURL     string                              // Associated mint URL
    Unit        string                              // Unit (e.g., "sat")
    Active      bool                                // Whether keyset is active
    PublicKeys  map[uint64]*secp256k1.PublicKey    // Public keys by denomination
    Counter     uint32                              // Deterministic counter
    InputFeePpk uint                                // Input fee per proof
}
```

### Keyset Derivation

Keyset IDs are derived deterministically from the public keys:

```go
func DeriveKeysetId(keyset PublicKeys) string {
    // 1. Sort public keys by denomination
    // 2. Concatenate all public keys
    // 3. SHA256 hash the concatenated keys
    // 4. Take first 14 characters of hex-encoded hash
    // 5. Prefix with version byte "00"
    return "00" + hex.EncodeToString(hash.Sum(nil))[:14]
}
```

## Keyset Lifecycle

### 1. Keyset Discovery

When a wallet interacts with a mint, it discovers keysets through API calls:

```go
// Get all keysets (active and inactive)
keysetsResponse, err := client.GetAllKeysets(mintURL)

// Get specific keyset by ID
keysetResponse, err := client.GetKeysetById(mintURL, keysetId)
```

### 2. Keyset Validation

Each keyset is validated before use:

```go
func GetKeysetKeys(mintURL, id string) (crypto.PublicKeys, error) {
    keysetsResponse, err := client.GetKeysetById(mintURL, id)
    if err != nil {
        return nil, err
    }

    // Derive ID from received keys
    derivedId := crypto.DeriveKeysetId(keysetsResponse.Keysets[0].Keys)
    if id != derivedId {
        return nil, fmt.Errorf("Got invalid keyset. Derived id: '%v' but got '%v'", derivedId, id)
    }

    return keysetsResponse.Keysets[0].Keys, nil
}
```

### 3. Keyset Storage

Keysets are stored in the wallet database with full metadata:

```go
// Storage interface
type WalletDB interface {
    SaveKeyset(*crypto.WalletKeyset) error
    GetKeysets() crypto.KeysetsMap
    GetKeyset(string) *crypto.WalletKeyset
    UpdateKeysetMintURL(oldURL, newURL string) error
}

// BoltDB implementation
func (db *BoltDB) SaveKeyset(keyset *crypto.WalletKeyset) error {
    return db.bolt.Update(func(tx *bolt.Tx) error {
        keysetsBucket := tx.Bucket([]byte(KEYSETS_BUCKET))
        
        keysetBytes, err := json.Marshal(keyset)
        if err != nil {
            return err
        }
        
        return keysetsBucket.Put([]byte(keyset.Id), keysetBytes)
    })
}
```

## Keyset Caching Strategy

### Memory Management

The library implements an efficient memory management strategy for keysets:

```go
type walletMint struct {
    mintURL         string
    activeKeyset    crypto.WalletKeyset
    inactiveKeysets map[string]crypto.WalletKeyset  // Without public keys in memory
}
```

**Active Keysets**: Fully loaded in memory with all public keys
**Inactive Keysets**: Metadata only, public keys loaded on demand

### Cache Population

Keysets are cached during normal wallet operations:

```go
func (w *Wallet) AddMint(mint string) (*walletMint, error) {
    // Get active keyset
    activeKeyset, err := GetMintActiveKeyset(mintURL, w.unit)
    if err != nil {
        return nil, err
    }

    // Get inactive keysets
    inactiveKeysets, err := GetMintInactiveKeysets(mintURL, w.unit)
    if err != nil {
        return nil, err
    }

    // Save to database
    if err := w.db.SaveKeyset(activeKeyset); err != nil {
        return nil, err
    }
    for _, keyset := range inactiveKeysets {
        if err := w.db.SaveKeyset(&keyset); err != nil {
            return nil, err
        }
    }

    // Cache in memory
    newWalletMint := walletMint{mintURL, *activeKeyset, inactiveKeysets}
    w.mints[mintURL] = newWalletMint
    
    return &newWalletMint, nil
}
```

## Keyset Rotation

### Active Keyset Changes

Mints periodically rotate their active keysets. The wallet detects and handles these changes:

```go
func (w *Wallet) getActiveKeyset(mintURL string) (*crypto.WalletKeyset, error) {
    mint, ok := w.mints[mintURL]
    if !ok {
        // New mint, fetch active keyset
        return GetMintActiveKeyset(mintURL, w.unit)
    }

    // Check if active keyset has changed
    allKeysets, err := client.GetAllKeysets(mintURL)
    if err != nil {
        return nil, err
    }

    activeKeyset := mint.activeKeyset
    activeChanged := true
    
    for _, keyset := range allKeysets.Keysets {
        if keyset.Active && keyset.Id == activeKeyset.Id {
            activeChanged = false
            break
        }
    }

    if activeChanged {
        // Inactivate previous active keyset
        activeKeyset.Active = false
        mint.inactiveKeysets[activeKeyset.Id] = activeKeyset
        w.db.SaveKeyset(&activeKeyset)

        // Find and activate new keyset
        for _, keyset := range allKeysets.Keysets {
            if keyset.Active && keyset.Unit == w.unit.String() {
                // Load or create new active keyset
                newActiveKeyset := w.loadOrCreateKeyset(keyset)
                mint.activeKeyset = newActiveKeyset
                w.mints[mintURL] = mint
                break
            }
        }
    }

    return &activeKeyset, nil
}
```

### Inactive Keyset Management

Inactive keysets are kept for historical proofs but with optimized memory usage:

```go
func (w *Wallet) loadWalletMints() (map[string]walletMint, error) {
    keysets := w.db.GetKeysets()
    
    for mintURL, mintKeysets := range keysets {
        var activeKeyset crypto.WalletKeyset
        inactiveKeysets := make(map[string]crypto.WalletKeyset)
        
        for _, keyset := range mintKeysets {
            if keyset.Active {
                activeKeyset = keyset
            } else {
                // Remove public keys from memory for inactive keysets
                keyset.PublicKeys = make(map[uint64]*secp256k1.PublicKey)
                inactiveKeysets[keyset.Id] = keyset
            }
        }
        
        walletMints[mintURL] = walletMint{
            mintURL:         mintURL,
            activeKeyset:    activeKeyset,
            inactiveKeysets: inactiveKeysets,
        }
    }
    
    return walletMints, nil
}
```

## Offline Keyset Handling

### Current Issues

The main offline issue occurs in [`loadWalletMints()`](../wallet/wallet.go:1808):

```go
// Problem: This fails when offline
if len(keyset.PublicKeys) == 0 {
    publicKeys, err := GetKeysetKeys(keyset.MintURL, keyset.Id)
    if err != nil {
        return nil, err  // Crashes here when offline
    }
    keyset.PublicKeys = publicKeys
    w.db.SaveKeyset(&keyset)
}
```

### Offline Solutions

#### 1. Graceful Degradation

```go
func (w *Wallet) loadWalletMintsOffline() (map[string]walletMint, error) {
    keysets := w.db.GetKeysets()
    walletMints := make(map[string]walletMint)
    
    for mintURL, mintKeysets := range keysets {
        var activeKeyset crypto.WalletKeyset
        inactiveKeysets := make(map[string]crypto.WalletKeyset)
        
        for _, keyset := range mintKeysets {
            // Skip keysets without cached public keys when offline
            if len(keyset.PublicKeys) == 0 {
                if w.isOffline {
                    log.Printf("Skipping keyset %s - no cached keys in offline mode", keyset.Id)
                    continue
                } else {
                    // Try to fetch when online
                    publicKeys, err := GetKeysetKeys(keyset.MintURL, keyset.Id)
                    if err != nil {
                        log.Printf("Failed to fetch keyset %s: %v", keyset.Id, err)
                        continue
                    }
                    keyset.PublicKeys = publicKeys
                    w.db.SaveKeyset(&keyset)
                }
            }
            
            if keyset.Active {
                activeKeyset = keyset
            } else {
                keyset.PublicKeys = make(map[uint64]*secp256k1.PublicKey)
                inactiveKeysets[keyset.Id] = keyset
            }
        }
        
        // Only add mint if we have a valid active keyset
        if len(activeKeyset.PublicKeys) > 0 {
            walletMints[mintURL] = walletMint{
                mintURL:         mintURL,
                activeKeyset:    activeKeyset,
                inactiveKeysets: inactiveKeysets,
            }
        }
    }
    
    return walletMints, nil
}
```

#### 2. Keyset Validation

```go
func (w *Wallet) validateOfflineKeyset(keyset *crypto.WalletKeyset) error {
    // Check if keyset has required public keys
    if len(keyset.PublicKeys) == 0 {
        return fmt.Errorf("keyset %s has no cached public keys", keyset.Id)
    }
    
    // Validate keyset ID matches public keys
    derivedId := crypto.DeriveKeysetId(keyset.PublicKeys)
    if derivedId != keyset.Id {
        return fmt.Errorf("keyset %s has invalid public keys", keyset.Id)
    }
    
    // Check if keyset is too old (optional)
    if time.Since(keyset.CachedAt) > 24*time.Hour {
        log.Printf("Warning: keyset %s is stale", keyset.Id)
    }
    
    return nil
}
```

#### 3. Background Keyset Refresh

```go
func (w *Wallet) startKeysetRefresh() {
    go func() {
        ticker := time.NewTicker(30 * time.Minute)
        defer ticker.Stop()
        
        for {
            select {
            case <-ticker.C:
                if !w.isOffline {
                    w.refreshAllKeysets()
                }
            }
        }
    }()
}

func (w *Wallet) refreshAllKeysets() {
    for mintURL := range w.mints {
        if err := w.refreshMintKeysets(mintURL); err != nil {
            log.Printf("Failed to refresh keysets for %s: %v", mintURL, err)
        }
    }
}
```

## Keyset Security

### Validation Checks

All keysets undergo validation before use:

```go
func validateKeyset(keyset *crypto.WalletKeyset) error {
    // Check ID format
    if len(keyset.Id) != 16 || !strings.HasPrefix(keyset.Id, "00") {
        return fmt.Errorf("invalid keyset ID format: %s", keyset.Id)
    }
    
    // Validate public keys
    for amount, pubkey := range keyset.PublicKeys {
        if !isPowerOf2(amount) {
            return fmt.Errorf("invalid amount %d - must be power of 2", amount)
        }
        if pubkey == nil {
            return fmt.Errorf("nil public key for amount %d", amount)
        }
    }
    
    // Verify derived ID matches
    derivedId := crypto.DeriveKeysetId(keyset.PublicKeys)
    if derivedId != keyset.Id {
        return fmt.Errorf("keyset ID mismatch: expected %s, got %s", keyset.Id, derivedId)
    }
    
    return nil
}
```

### Keyset Integrity

Keysets are protected against tampering:

```go
func (w *Wallet) verifyKeysetIntegrity(keyset *crypto.WalletKeyset) error {
    // Re-derive ID from public keys
    derivedId := crypto.DeriveKeysetId(keyset.PublicKeys)
    if derivedId != keyset.Id {
        return fmt.Errorf("keyset integrity check failed for %s", keyset.Id)
    }
    
    // Check against known good keysets (if available)
    if storedKeyset := w.db.GetKeyset(keyset.Id); storedKeyset != nil {
        if !keysetEqual(keyset, storedKeyset) {
            return fmt.Errorf("keyset %s has been modified", keyset.Id)
        }
    }
    
    return nil
}
```

## Performance Optimizations

### Lazy Loading

Inactive keyset public keys are loaded only when needed:

```go
func (w *Wallet) getKeysetPublicKeys(keysetId string) (map[uint64]*secp256k1.PublicKey, error) {
    // Check if already in memory
    for _, mint := range w.mints {
        if mint.activeKeyset.Id == keysetId {
            return mint.activeKeyset.PublicKeys, nil
        }
        if inactiveKeyset, ok := mint.inactiveKeysets[keysetId]; ok {
            // Load public keys if not in memory
            if len(inactiveKeyset.PublicKeys) == 0 {
                publicKeys, err := GetKeysetKeys(inactiveKeyset.MintURL, keysetId)
                if err != nil {
                    return nil, err
                }
                inactiveKeyset.PublicKeys = publicKeys
                mint.inactiveKeysets[keysetId] = inactiveKeyset
            }
            return inactiveKeyset.PublicKeys, nil
        }
    }
    
    return nil, fmt.Errorf("keyset %s not found", keysetId)
}
```

### Batch Operations

Multiple keysets can be fetched in a single operation:

```go
func (w *Wallet) refreshMintKeysets(mintURL string) error {
    // Get all keysets from mint
    allKeysets, err := client.GetAllKeysets(mintURL)
    if err != nil {
        return err
    }
    
    // Update local keysets
    for _, remoteKeyset := range allKeysets.Keysets {
        localKeyset := w.db.GetKeyset(remoteKeyset.Id)