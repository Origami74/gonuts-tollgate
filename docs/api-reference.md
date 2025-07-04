# API Reference

## Overview

This document provides comprehensive API documentation for the Gonuts library, covering all public types, methods, and functions.

## Package Structure

```
github.com/elnosh/gonuts/
├── cashu/          # Core protocol types
├── crypto/         # Cryptographic operations
├── wallet/         # Wallet operations
├── mint/           # Mint operations
└── wallet/client/  # Network client
```

## Cashu Package

### Core Types

#### Proof
```go
type Proof struct {
    Amount  uint64 `json:"amount"`
    Id      string `json:"id"`      // Keyset ID
    Secret  string `json:"secret"`
    C       string `json:"C"`       // Signature
    Witness string `json:"witness,omitempty"`
    DLEQ    *DLEQProof `json:"dleq,omitempty"`
}
```

**Methods:**
- `Amount() uint64` - Returns proof amount
- `Valid() bool` - Validates proof structure

#### Proofs
```go
type Proofs []Proof
```

**Methods:**
- `Amount() uint64` - Returns total amount of all proofs
- `AmountChecked() (uint64, error)` - Returns amount with overflow check

#### Token Interface
```go
type Token interface {
    Proofs() Proofs
    Mint() string
    Amount() uint64
    Serialize() (string, error)
}
```

#### TokenV4
```go
type TokenV4 struct {
    TokenProofs []TokenV4Proof `json:"t"`
    Memo        string         `json:"d,omitempty"`
    MintURL     string         `json:"m"`
    Unit        string         `json:"u"`
}
```

**Methods:**
- `Proofs() Proofs` - Extract proofs from token
- `Mint() string` - Get mint URL
- `Amount() uint64` - Get total amount
- `Serialize() (string, error)` - Serialize to string

#### BlindedMessage
```go
type BlindedMessage struct {
    Amount  uint64 `json:"amount"`
    B_      string `json:"B_"`    // Blinded message
    Id      string `json:"id"`    // Keyset ID
    Witness string `json:"witness,omitempty"`
}
```

#### BlindedSignature
```go
type BlindedSignature struct {
    Amount uint64 `json:"amount"`
    C_     string `json:"C_"`     // Blinded signature
    Id     string `json:"id"`     // Keyset ID
    DLEQ   *DLEQProof `json:"dleq,omitempty"`
}
```

### Functions

#### Token Operations
```go
func DecodeToken(tokenstr string) (Token, error)
func NewTokenV4(proofs Proofs, mint string, unit Unit, includeDLEQ bool) (TokenV4, error)
func NewTokenV3(proofs Proofs, mint string, unit Unit, includeDLEQ bool) (TokenV3, error)
```

#### Utility Functions
```go
func AmountSplit(amount uint64) []uint64
func CheckDuplicateProofs(proofs Proofs) bool
func CheckDuplicateBlindedMessages(bms BlindedMessages) bool
func GenerateRandomQuoteId() (string, error)
```

## Wallet Package

### Types

#### Wallet
```go
type Wallet struct {
    // Private fields
}
```

**Methods:**

##### Initialization
```go
func LoadWallet(config Config) (*Wallet, error)
func (w *Wallet) Shutdown() error
```

##### Balance Operations
```go
func (w *Wallet) GetBalance() uint64
func (w *Wallet) GetBalanceByMints() map[string]uint64
func (w *Wallet) GetProofs() cashu.Proofs
func (w *Wallet) GetProofsByKeysetId(keysetId string) cashu.Proofs
```

##### Mint Operations
```go
func (w *Wallet) RequestMint(amount uint64, mintURL string) (*nut04.PostMintQuoteBolt11Response, error)
func (w *Wallet) MintQuoteState(quoteId string) (*nut04.PostMintQuoteBolt11Response, error)
func (w *Wallet) MintTokens(quoteId string) (uint64, error)
```

##### Send Operations
```go
func (w *Wallet) Send(amount uint64, mintURL string, includeFees bool) (cashu.Proofs, error)
func (w *Wallet) SendToPubkey(amount uint64, mintURL string, pubkey *btcec.PublicKey, tags *nut11.P2PKTags, includeFees bool) (cashu.Proofs, error)
func (w *Wallet) HTLCLockedProofs(amount uint64, mintURL string, preimage string, tags *nut11.P2PKTags, includeFees bool) (cashu.Proofs, error)
```

##### Receive Operations
```go
func (w *Wallet) Receive(token cashu.Token, swapToTrusted bool) (uint64, error)
func (w *Wallet) ReceiveP2PK(token cashu.Token, privKey *btcec.PrivateKey) (uint64, error)
func (w *Wallet) ReceiveHTLC(token cashu.Token, preimage string) (uint64, error)
```

##### Melt Operations
```go
func (w *Wallet) RequestMeltQuote(invoice string, mintURL string) (*nut05.PostMeltQuoteBolt11Response, error)
func (w *Wallet) MeltQuoteState(quoteId string) (*nut05.PostMeltQuoteBolt11Response, error)
func (w *Wallet) Melt(quoteId string) (*nut05.PostMeltBolt11Response, error)
```

##### Mint Management
```go
func (w *Wallet) AddMint(mint string) (*walletMint, error)
func (w *Wallet) TrustedMints() []string
func (w *Wallet) CurrentMint() string
func (w *Wallet) UpdateMintURL(oldURL, newURL string) error
```

##### Restore Operations
```go
func (w *Wallet) Restore(mintURL string, keysetIds []string) (uint64, error)
```

##### Utility Methods
```go
func (w *Wallet) Mnemonic() string
func (w *Wallet) GetReceivePubkey() *btcec.PublicKey
```

#### Config
```go
type Config struct {
    WalletPath     string
    CurrentMintURL string
}
```

### Functions

#### Initialization
```go
func InitStorage(path string) (storage.WalletDB, error)
```

#### Keyset Management
```go
func GetMintActiveKeyset(mintURL string, unit cashu.Unit) (*crypto.WalletKeyset, error)
func GetMintInactiveKeysets(mintURL string, unit cashu.Unit) (map[string]crypto.WalletKeyset, error)
func GetKeysetKeys(mintURL, id string) (crypto.PublicKeys, error)
```

## Crypto Package

### Types

#### WalletKeyset
```go
type WalletKeyset struct {
    Id          string
    MintURL     string
    Unit        string
    Active      bool
    PublicKeys  map[uint64]*secp256k1.PublicKey
    Counter     uint32
    InputFeePpk uint
}
```

#### MintKeyset
```go
type MintKeyset struct {
    Id                string
    Unit              string
    Active            bool
    DerivationPathIdx uint32
    Keys              map[uint64]KeyPair
    InputFeePpk       uint
}
```

**Methods:**
- `PublicKeys() PublicKeys` - Get public keys

#### KeyPair
```go
type KeyPair struct {
    PrivateKey *secp256k1.PrivateKey
    PublicKey  *secp256k1.PublicKey
}
```

### Functions

#### Keyset Operations
```go
func GenerateKeyset(master *hdkeychain.ExtendedKey, index uint32, inputFeePpk uint, active bool) (*MintKeyset, error)
func DeriveKeysetId(keyset PublicKeys) string
func DeriveKeysetPath(key *hdkeychain.ExtendedKey, index uint32) (*hdkeychain.ExtendedKey, error)
```

#### Cryptographic Operations
```go
func BlindMessage(secret string, r *secp256k1.PrivateKey) (*secp256k1.PublicKey, *secp256k1.PrivateKey, error)
func UnblindSignature(C_ *secp256k1.PublicKey, r *secp256k1.PrivateKey, A *secp256k1.PublicKey) *secp256k1.PublicKey
func HashToCurve(message []byte) *secp256k1.PublicKey
```

## Wallet Client Package

### Functions

#### Mint Information
```go
func GetMintInfo(mintURL string) (*nut06.MintInfo, error)
```

#### Keyset Operations
```go
func GetActiveKeysets(mintURL string) (*nut01.GetKeysResponse, error)
func GetAllKeysets(mintURL string) (*nut02.GetKeysetsResponse, error)
func GetKeysetById(mintURL, id string) (*nut01.GetKeysResponse, error)
```

#### Mint Operations
```go
func PostMintQuoteBolt11(mintURL string, req nut04.PostMintQuoteBolt11Request) (*nut04.PostMintQuoteBolt11Response, error)
func GetMintQuoteState(mintURL, quoteId string) (*nut04.PostMintQuoteBolt11Response, error)
func PostMintBolt11(mintURL string, req nut04.PostMintBolt11Request) (*nut04.PostMintBolt11Response, error)
```

#### Melt Operations
```go
func PostMeltQuoteBolt11(mintURL string, req nut05.PostMeltQuoteBolt11Request) (*nut05.PostMeltQuoteBolt11Response, error)
func GetMeltQuoteState(mintURL, quoteId string) (*nut05.PostMeltQuoteBolt11Response, error)
func PostMeltBolt11(mintURL string, req nut05.PostMeltBolt11Request) (*nut05.PostMeltBolt11Response, error)
```

#### Swap Operations
```go
func PostSwap(mintURL string, req nut03.PostSwapRequest) (*nut03.PostSwapResponse, error)
```

#### State Operations
```go
func PostCheckState(mintURL string, req nut07.PostCheckStateRequest) (*nut07.PostCheckStateResponse, error)
```

## Storage Package

### Interfaces

#### WalletDB
```go
type WalletDB interface {
    // Seed operations
    SaveMnemonicSeed(string, []byte)
    GetSeed() []byte
    GetMnemonic() string

    // Proof operations
    SaveProofs(cashu.Proofs) error
    GetProofs() cashu.Proofs
    GetProofsByKeysetId(string) cashu.Proofs
    DeleteProof(string) error

    // Pending proof operations
    AddPendingProofs(cashu.Proofs) error
    GetPendingProofs() []DBProof
    DeletePendingProofs([]string) error

    // Keyset operations
    SaveKeyset(*crypto.WalletKeyset) error
    GetKeysets() crypto.KeysetsMap
    GetKeyset(string) *crypto.WalletKeyset
    IncrementKeysetCounter(string, uint32) error
    GetKeysetCounter(string) uint32

    // Quote operations
    SaveMintQuote(MintQuote) error
    GetMintQuotes() []MintQuote
    GetMintQuoteById(string) *MintQuote
    SaveMeltQuote(MeltQuote) error
    GetMeltQuotes() []MeltQuote
    GetMeltQuoteById(string) *MeltQuote

    // Utility
    Close() error
}
```

### Types

#### DBProof
```go
type DBProof struct {
    Y           string           `json:"y"`
    Amount      uint64           `json:"amount"`
    Id          string           `json:"id"`
    Secret      string           `json:"secret"`
    C           string           `json:"C"`
    DLEQ        *cashu.DLEQProof `json:"dleq,omitempty"`
    MeltQuoteId string           `json:"quote_id"`
}
```

#### MintQuote
```go
type MintQuote struct {
    QuoteId        string
    Mint           string
    Method         string
    State          nut04.State
    Unit           string
    PaymentRequest string
    Amount         uint64
    CreatedAt      int64
    SettledAt      int64
    QuoteExpiry    uint64
    PrivateKey     *secp256k1.PrivateKey
}
```

#### MeltQuote
```go
type MeltQuote struct {
    QuoteId        string
    Mint           string
    Method         string
    State          nut05.State
    Unit           string
    PaymentRequest string
    Amount         uint64
    FeeReserve     uint64
    Preimage       string
    CreatedAt      int64
    SettledAt      int64
    QuoteExpiry    uint64
}
```

### Functions

#### BoltDB Storage
```go
func InitBolt(path string) (*BoltDB, error)
```

## Error Types

### Common Errors

```go
var (
    ErrMintNotExist            = errors.New("mint does not exist")
    ErrInsufficientMintBalance = errors.New("not enough funds in selected mint")
    ErrQuoteNotFound           = errors.New("quote not found")
    ErrInvalidTokenV3          = errors.New("invalid V3 token")
    ErrInvalidTokenV4          = errors.New("invalid V4 token")
    ErrInvalidUnit             = errors.New("invalid unit")
    ErrAmountOverflows         = errors.New("amount overflows")
)
```

### Cashu Error Codes

```go
const (
    StandardErrCode                    CashuErrCode = 10000
    BlindedMessageAlreadySignedErrCode CashuErrCode = 10002
    InvalidProofErrCode                CashuErrCode = 10003
    SecretTooLongErrCode               CashuErrCode = 10004
    ProofAlreadyUsedErrCode            CashuErrCode = 11001
    // ... more error codes
)
```

## Constants

### Units
```go
const (
    Sat Unit = iota
)
```

### Protocol Constants
```go
const (
    BOLT11_METHOD     = "bolt11"
    MAX_SECRET_LENGTH = 512
)
```

### Cryptographic Constants
```go
const MAX_ORDER = 60 // Maximum number of keys in a keyset
```

## Usage Examples

### Basic Wallet Operations

```go
// Initialize wallet
config := wallet.Config{
    WalletPath:     "./wallet",
    CurrentMintURL: "https://mint.example.com",
}
w, err := wallet.LoadWallet(config)
if err != nil {
    log.Fatal(err)
}
defer w.Shutdown()

// Check balance
balance := w.GetBalance()
fmt.Printf("Balance: %d sats\n", balance)

// Send tokens
proofs, err := w.Send(1000, w.CurrentMint(), true)
if err != nil {
    log.Fatal(err)
}

// Create token
token, err := cashu.NewTokenV4(proofs, w.CurrentMint(), cashu.Sat, false)
if err != nil {
    log.Fatal(err)
}

tokenStr, err := token.Serialize()
if