package main

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/Origami74/gonuts-tollgate/cashu"
	"github.com/Origami74/gonuts-tollgate/cashu/nuts/nut04"
	"github.com/Origami74/gonuts-tollgate/cashu/nuts/nut05"
	"github.com/Origami74/gonuts-tollgate/cashu/nuts/nut11"
	"github.com/Origami74/gonuts-tollgate/cashu/nuts/nut17"
	"github.com/Origami74/gonuts-tollgate/wallet"
	"github.com/Origami74/gonuts-tollgate/wallet/client"
	"github.com/Origami74/gonuts-tollgate/wallet/submanager"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/joho/godotenv"
	decodepay "github.com/nbd-wtf/ln-decodepay"
	"github.com/urfave/cli/v2"
)

var nutw *wallet.Wallet

func walletConfig() (wallet.Config, error) {
	env := envPath()
	if len(env) > 0 {
		err := godotenv.Load(env)
		if err != nil {
			// if no .env file to load, use default
			return wallet.Config{
				WalletPath:     defaultWalletPath(),
				CurrentMintURL: "http://127.0.0.1:3338",
			}, nil
		}
	}

	walletPath := os.Getenv("WALLET_PATH")
	if len(walletPath) == 0 {
		walletPath = defaultWalletPath()
	}

	mint := os.Getenv("MINT_URL")
	if len(mint) == 0 {
		mint = "http://127.0.0.1:3338"
	}
	config := wallet.Config{WalletPath: walletPath, CurrentMintURL: mint}

	return config, nil
}

func defaultWalletPath() string {
	homedir, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}

	return filepath.Join(homedir, ".gonuts", "wallet")
}

func envPath() string {
	defaultPath := defaultWalletPath()

	// if .env file present at default wallet path then use that
	// if not, look at current dir
	envPath := filepath.Join(defaultPath, ".env")
	if _, err := os.Stat(envPath); err != nil {
		wd, err := os.Getwd()
		if err != nil {
			envPath = ""
		} else {
			envPath = filepath.Join(wd, ".env")
		}
	}
	return envPath
}

func setupWallet(ctx *cli.Context) error {
	config, err := walletConfig()
	if err != nil {
		printErr(err)
	}

	nutw, err = wallet.LoadWallet(config)
	if err != nil {
		printErr(err)
	}
	return nil
}

func main() {
	app := &cli.App{
		Name:  "nutw",
		Usage: "cashu wallet",
		Commands: []*cli.Command{
			balanceCmd,
			mintCmd,
			sendCmd,
			receiveCmd,
			payCmd,
			pendingCmd,
			quotesCmd,
			p2pkLockCmd,
			mnemonicCmd,
			restoreCmd,
			currentMintCmd,
			updateMintCmd,
			decodeCmd,
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

const (
	pendingFlag = "pending"
)

var balanceCmd = &cli.Command{
	Name:   "balance",
	Usage:  "Wallet balance",
	Before: setupWallet,
	Action: getBalance,
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:               pendingFlag,
			Usage:              "show pending balance",
			DisableDefaultText: true,
		},
	},
}

func getBalance(ctx *cli.Context) error {
	balanceByMints := nutw.GetBalanceByMints()
	fmt.Printf("Balance by mint:\n\n")
	totalBalance := uint64(0)

	mints := nutw.TrustedMints()
	slices.Sort(mints)

	for i, mint := range mints {
		balance := balanceByMints[mint]
		fmt.Printf("Mint %v: %v ---- balance: %v sats\n", i+1, mint, balance)
		totalBalance += balance
	}

	fmt.Printf("\nTotal balance: %v sats\n", totalBalance)

	if ctx.Bool(pendingFlag) {
		pendingBalance := nutw.PendingBalance()
		fmt.Printf("Pending balance: %v sats\n", pendingBalance)
	}

	return nil
}

const (
	preimageFlag = "preimage"
)

var receiveCmd = &cli.Command{
	Name:      "receive",
	Usage:     "Receive token",
	ArgsUsage: "[TOKEN]",
	Before:    setupWallet,
	Action:    receive,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  preimageFlag,
			Usage: "preimage if receiving ecash HTLC",
		},
	},
}

func receive(ctx *cli.Context) error {
	args := ctx.Args()
	if args.Len() < 1 {
		printErr(errors.New("token not provided"))
	}
	serializedToken := args.First()

	token, err := cashu.DecodeToken(serializedToken)
	if err != nil {
		printErr(err)
	}
	mintURL := token.Mint()

	if ctx.IsSet(preimageFlag) {
		preimage := ctx.String(preimageFlag)
		receivedAmount, err := nutw.ReceiveHTLC(token, preimage)
		if err != nil {
			printErr(err)
		}
		fmt.Printf("%v sats received from ecash HTLC\n", receivedAmount)
		return nil
	}

	swap := true
	trustedMints := nutw.TrustedMints()

	isTrusted := slices.Contains(trustedMints, mintURL)
	if !isTrusted {
		fmt.Printf("Token received comes from an untrusted mint: %v. Do you wish to trust this mint? (y/n) ", mintURL)

		reader := bufio.NewReader(os.Stdin)
		input, err := reader.ReadString('\n')
		if err != nil {
			log.Fatal("error reading input, please try again")
		}

		input = strings.ToLower(strings.TrimSpace(input))
		if input == "y" || input == "yes" {
			fmt.Println("Token from unknown mint will be added")
			swap = false
		} else {
			fmt.Println("Token will be swapped to your default trusted mint")
		}
	} else {
		// if it comes from an already trusted mint, do not swap
		swap = false
	}

	receivedAmount, err := nutw.Receive(token, swap)
	if err != nil {
		printErr(err)
	}

	fmt.Printf("%v sats received\n", receivedAmount)
	return nil
}

const (
	invoiceFlag = "invoice"
	mintFlag    = "mint"
)

var mintCmd = &cli.Command{
	Name:      "mint",
	Usage:     "Request mint quote. It will return a lightning invoice to be paid",
	ArgsUsage: "[AMOUNT]",
	Before:    setupWallet,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  invoiceFlag,
			Usage: "Specify paid invoice to mint tokens",
		},
		&cli.StringFlag{
			Name:  mintFlag,
			Usage: "Specify mint from which to request mint quote",
		},
	},
	Action: mint,
}

func mint(ctx *cli.Context) error {
	// if paid invoice was passed, request tokens from mint
	if ctx.IsSet(invoiceFlag) {
		err := mintTokens(ctx.String(invoiceFlag))
		if err != nil {
			printErr(err)
		}
		return nil
	}

	args := ctx.Args()
	if args.Len() < 1 {
		printErr(errors.New("specify an amount to mint"))
	}
	amount, err := strconv.ParseUint(args.First(), 10, 64)
	if err != nil {
		return errors.New("invalid amount")
	}

	mint := nutw.CurrentMint()
	if ctx.IsSet(mintFlag) {
		mint = ctx.String(mintFlag)
	}

	err = requestMint(amount, mint)
	if err != nil {
		printErr(err)
	}

	return nil
}

func requestMint(amount uint64, mintURL string) error {
	mintResponse, err := nutw.RequestMint(amount, mintURL)
	if err != nil {
		return err
	}

	fmt.Printf("invoice: %v\n\n", mintResponse.Request)

	subMananger, err := submanager.NewSubscriptionManager(mintURL)
	if err != nil {
		fmt.Println("after paying the invoice you can redeem the ecash by doing 'nutw mint --invoice [invoice]'")
		return nil
	}
	defer subMananger.Close()

	errChan := make(chan error)
	go subMananger.Run(errChan)

	subscription, err := subMananger.Subscribe(nut17.Bolt11MintQuote, []string{mintResponse.Quote})
	if err != nil {
		fmt.Println("after paying the invoice you can redeem the ecash by doing 'nutw mint --invoice [invoice]'")
		return nil
	}

	fmt.Println("checking if invoice gets paid...")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, os.Kill, syscall.SIGTERM)
	done := make(chan struct{})

	go func() {
		select {
		case err := <-errChan:
			fmt.Printf("error reading from websocket connection: %v\n\n", err)
			fmt.Println("after paying the invoice you can redeem the ecash by doing 'nutw mint --invoice [invoice]'")
			os.Exit(1)
		case <-sigChan:
			fmt.Println("\nterminating... after paying the invoice you can also redeem the ecash by doing 'nutw mint --invoice [invoice]'")
			os.Exit(0)
		case <-done:
			return
		}
	}()

	for {
		notification, err := subscription.Read()
		if err != nil {
			return err
		}

		var mintQuote nut04.PostMintQuoteBolt11Response
		if err := json.Unmarshal(notification.Params.Payload, &mintQuote); err != nil {
			return err
		}

		if mintQuote.State == nut04.Paid {
			mintedAmount, err := nutw.MintTokens(mintResponse.Quote)
			if err != nil {
				return err
			}

			fmt.Printf("%v sats successfully minted\n", mintedAmount)

			if err := subMananger.CloseSubscripton(subscription.SubId()); err != nil {
				return err
			}
			done <- struct{}{}
			return nil
		}
	}
}

func mintTokens(paymentRequest string) error {
	quote, err := nutw.GetMintQuoteByPaymentRequest(paymentRequest)
	if err != nil {
		return err
	}

	mintedAmount, err := nutw.MintTokens(quote.QuoteId)
	if err != nil {
		return err
	}

	fmt.Printf("%v sats successfully minted\n", mintedAmount)
	return nil
}

const (
	p2pklockFlag     = "lock-p2pk"
	htlcLockFlag     = "lock-htlc"
	requiredSigsFlag = "required-signatures"
	pubkeysFlag      = "pubkeys"
	locktimeFlag     = "locktime"
	refundKeysFlag   = "refund-keys"
	noFeesFlag       = "no-fees"
	legacyFlag       = "legacy"
	includeDLEQFlag  = "include-dleq"
)

var sendCmd = &cli.Command{
	Name:      "send",
	Usage:     "Generates token to be sent for the specified amount",
	ArgsUsage: "[AMOUNT]",
	Before:    setupWallet,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  p2pklockFlag,
			Usage: "generate ecash locked to a public key",
		},
		&cli.StringFlag{
			Name:  htlcLockFlag,
			Usage: "generate ecash locked to hash of preimage",
		},

		// --------------- Optional lock flags category ----------------------
		&cli.IntFlag{
			Name:     requiredSigsFlag,
			Usage:    "number of required signatures",
			Category: "Optional lock flags for P2PK or HTLC",
		},
		&cli.StringSliceFlag{
			Name:     pubkeysFlag,
			Usage:    "additional public keys that can provide signatures.",
			Category: "Optional lock flags for P2PK or HTLC",
		},
		&cli.Int64Flag{
			Name:     locktimeFlag,
			Usage:    "Unix timestamp for P2PK or HTLC to expire",
			Category: "Optional lock flags for P2PK or HTLC",
		},
		&cli.StringSliceFlag{
			Name:     refundKeysFlag,
			Usage:    "list of public keys that can sign after locktime",
			Category: "Optional lock flags for P2PK or HTLC",
		},
		// --------------- Optional lock flags category ----------------------

		&cli.BoolFlag{
			Name:               noFeesFlag,
			Usage:              "do not include fees for receiver in the token generated",
			DisableDefaultText: true,
		},
		&cli.BoolFlag{
			Name:               legacyFlag,
			Usage:              "generate token in legacy (V3) format",
			DisableDefaultText: true,
		},
		&cli.BoolFlag{
			Name:               includeDLEQFlag,
			Usage:              "include DLEQ proofs",
			DisableDefaultText: true,
		},
	},
	Action: send,
}

func send(ctx *cli.Context) error {
	args := ctx.Args()
	if args.Len() < 1 {
		printErr(errors.New("specify an amount to send"))
	}
	amountArg := args.First()
	sendAmount, err := strconv.ParseUint(amountArg, 10, 64)
	if err != nil {
		printErr(err)
	}

	selectedMint := promptMintSelection("send")

	includeFees := true
	if ctx.Bool(noFeesFlag) {
		includeFees = false
	}

	var proofsToSend cashu.Proofs

	// if either P2PK or HTLC, read optional flags
	if ctx.IsSet(p2pklockFlag) || ctx.IsSet(htlcLockFlag) {
		tags := nut11.P2PKTags{
			NSigs:    ctx.Int(requiredSigsFlag),
			Locktime: ctx.Int64(locktimeFlag),
		}

		for _, pubkey := range ctx.StringSlice(pubkeysFlag) {
			pubkeyBytes, err := hex.DecodeString(pubkey)
			if err != nil {
				printErr(err)
			}

			publicKey, err := secp256k1.ParsePubKey(pubkeyBytes)
			if err != nil {
				printErr(err)
			}
			tags.Pubkeys = append(tags.Pubkeys, publicKey)
		}

		for _, pubkey := range ctx.StringSlice(refundKeysFlag) {
			pubkeyBytes, err := hex.DecodeString(pubkey)
			if err != nil {
				printErr(err)
			}

			publicKey, err := secp256k1.ParsePubKey(pubkeyBytes)
			if err != nil {
				printErr(err)
			}
			tags.Refund = append(tags.Refund, publicKey)
		}

		if ctx.IsSet(p2pklockFlag) {
			lockpubkey := ctx.String(p2pklockFlag)
			lockbytes, err := hex.DecodeString(lockpubkey)
			if err != nil {
				printErr(err)
			}
			pubkey, err := secp256k1.ParsePubKey(lockbytes)
			if err != nil {
				printErr(err)
			}
			proofsToSend, err = nutw.SendToPubkey(sendAmount, selectedMint, pubkey, &tags, includeFees)
			if err != nil {
				printErr(err)
			}
		} else {
			preimage := ctx.String(htlcLockFlag)
			proofsToSend, err = nutw.HTLCLockedProofs(sendAmount, selectedMint, preimage, &tags, includeFees)
			if err != nil {
				printErr(err)
			}
		}
	} else {
		proofsToSend, err = nutw.Send(sendAmount, selectedMint, includeFees)
		if err != nil {
			printErr(err)
		}
	}

	includeDLEQ := false
	if ctx.Bool(includeDLEQFlag) {
		includeDLEQ = true
	}

	var token cashu.Token
	if ctx.Bool(legacyFlag) {
		token, _ = cashu.NewTokenV3(proofsToSend, selectedMint, cashu.Sat, includeDLEQ)
	} else {
		token, err = cashu.NewTokenV4(proofsToSend, selectedMint, cashu.Sat, includeDLEQ)
		if err != nil {
			printErr(fmt.Errorf("could not serialize token: %v", err))
		}
	}

	tokenString, err := token.Serialize()
	if err != nil {
		printErr(fmt.Errorf("could not serialize token: %v", err))
	}
	fmt.Printf("%v\n", tokenString)

	return nil
}

const (
	multimintFlag = "multimint"
)

var payCmd = &cli.Command{
	Name:      "pay",
	Usage:     "Pay a lightning invoice",
	ArgsUsage: "[INVOICE]",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  multimintFlag,
			Usage: "pay invoice using funds from multiple mints",
		},
	},
	Before: setupWallet,
	Action: pay,
}

func pay(ctx *cli.Context) error {
	args := ctx.Args()
	if args.Len() < 1 {
		printErr(errors.New("specify a lightning invoice to pay"))
	}
	invoice := args.First()

	// check invoice passed is valid
	bolt11, err := decodepay.Decodepay(invoice)
	if err != nil {
		printErr(fmt.Errorf("invalid invoice: %v", err))
	}

	if ctx.Bool(multimintFlag) {
		balanceByMints := nutw.GetBalanceByMints()
		mints := nutw.TrustedMints()
		slices.Sort(mints)
		split := make(map[string]uint64)

		for i, mint := range mints {
			balance := balanceByMints[mint]
			fmt.Printf("Mint %v: %v ---- balance: %v sats\n", i+1, mint, balance)
		}

		invoiceAmount := bolt11.MSatoshi / 1000
		fmt.Printf("\nAmount of invoice to pay: %v", invoiceAmount)

		amountStillToPay := invoiceAmount
		for {
			fmt.Printf("\nSelect a mint (1-%v) from which to partially pay the invoice: ", len(mints))
			reader := bufio.NewReader(os.Stdin)
			input, err := reader.ReadString('\n')
			if err != nil {
				log.Fatal("error reading input, please try again")
			}
			num, err := strconv.Atoi(input[:len(input)-1])
			if err != nil {
				printErr(errors.New("invalid number provided"))
			}
			if num <= 0 || num > len(mints) {
				printErr(errors.New("invalid mint selected"))
			}

			selectedMint := mints[num-1]
			mintBalance := balanceByMints[selectedMint]
			fmt.Printf("Select the amount to use from the mint's balance (%v): ", mintBalance)
			input, err = reader.ReadString('\n')
			if err != nil {
				log.Fatal("error reading input, please try again")
			}

			amountToUse, err := strconv.ParseInt(input[:len(input)-1], 10, 64)
			if err != nil {
				printErr(errors.New("invalid number provided"))
			}

			if amountToUse < 0 {
				printErr(errors.New("amount has to be greater than 0"))
			}
			if uint64(amountToUse) > mintBalance {
				errmsg := fmt.Errorf(`amount specified '%v' is greater than balance '%v' for that mint`,
					amountToUse, mintBalance)
				printErr(errmsg)
			}

			// amount needs to be in msat
			split[selectedMint] = uint64(amountToUse * 1000)
			amountStillToPay -= amountToUse

			if amountStillToPay > 0 {
				fmt.Printf("\nStill need to select amount %v", amountStillToPay)
			} else if amountStillToPay == 0 {
				meltResponses, err := nutw.MultiMintPayment(invoice, split)
				if err != nil {
					printErr(fmt.Errorf("could not do multimint payment: %v", err))
				}

				for _, response := range meltResponses {
					if response.State == nut05.Pending {
						fmt.Println("payment is pending")
						return nil
					} else if response.State == nut05.Unpaid {
						fmt.Println("could not do multimint payment")
						return nil
					}
				}

				if meltResponses[0].State == nut05.Paid {
					fmt.Printf("Multimint payment successful! Preimage: %v\n", meltResponses[0].Preimage)
					return nil
				}

			} else {
				printErr(errors.New("aggregate amount selected cannot be higher than invoice amount"))
			}
		}

	} else {
		// do regular single mint payment if multimint not set
		selectedMint := promptMintSelection("pay invoice")
		meltQuote, err := nutw.RequestMeltQuote(invoice, selectedMint)
		if err != nil {
			printErr(err)
		}

		meltResult, err := nutw.Melt(meltQuote.Quote)
		if err != nil {
			printErr(err)
		}

		switch meltResult.State {
		case nut05.Paid:
			fmt.Printf("Invoice paid sucessfully. Preimage: %v\n", meltResult.Preimage)
		case nut05.Pending:
			fmt.Println("payment is pending")
		case nut05.Unpaid:
			fmt.Println("mint could not pay invoice")
		}
	}

	return nil
}

const (
	removeFlag  = "remove"
	reclaimFlag = "reclaim"
)

var pendingCmd = &cli.Command{
	Name:   "pending",
	Usage:  "Manage pending proofs",
	Before: setupWallet,
	Action: pending,
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:               removeFlag,
			Usage:              "remove spent proofs",
			DisableDefaultText: true,
		},
		&cli.BoolFlag{
			Name:               reclaimFlag,
			Usage:              "reclaim unspent pending proofs",
			DisableDefaultText: true,
		},
	},
}

func pending(ctx *cli.Context) error {
	if ctx.Bool(removeFlag) {
		if err := nutw.RemoveSpentProofs(); err != nil {
			printErr(err)
		}
		pendingBalance := nutw.PendingBalance()
		fmt.Printf("Pending balance: %v sats\n", pendingBalance)
		return nil
	}

	if ctx.Bool(reclaimFlag) {
		amountReclaimed, err := nutw.ReclaimUnspentProofs()
		if err != nil {
			printErr(err)
		}

		if amountReclaimed > 0 {
			fmt.Printf("reclaimed %v sats\n", amountReclaimed)
		} else {
			fmt.Println("no amount to reclaim")
		}
		return nil
	}

	pendingBalance := nutw.PendingBalance()
	fmt.Printf("Pending balance: %v sats\n", pendingBalance)
	return nil
}

const (
	checkFlag = "check"
)

var quotesCmd = &cli.Command{
	Name:   "quotes",
	Usage:  "list and check status of pending melt quotes",
	Before: setupWallet,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  checkFlag,
			Usage: "check state of quote",
		},
	},
	Action: quotes,
}

func quotes(ctx *cli.Context) error {
	pendingQuotes := nutw.GetPendingMeltQuotes()

	if ctx.IsSet(checkFlag) {
		quote := ctx.String(checkFlag)

		quoteResponse, err := nutw.CheckMeltQuoteState(quote)
		if err != nil {
			printErr(err)
		}

		switch quoteResponse.State {
		case nut05.Paid:
			fmt.Printf("Invoice for quote '%v' was paid. Preimage: %v\n", quote, quoteResponse.Preimage)
		case nut05.Pending:
			fmt.Println("payment is still pending")
		case nut05.Unpaid:
			fmt.Println("quote was not paid")
		}

		return nil
	}

	if len(pendingQuotes) > 0 {
		fmt.Println("Pending quotes: ")
		for _, quote := range pendingQuotes {
			fmt.Printf("ID: %v\n", quote)
		}
	} else {
		fmt.Println("no pending quotes")
	}

	return nil
}

var p2pkLockCmd = &cli.Command{
	Name:   "p2pk-lock",
	Usage:  "Retrieves a public key to which ecash can be locked",
	Before: setupWallet,
	Action: p2pkLock,
}

func p2pkLock(ctx *cli.Context) error {
	lockpubkey := nutw.GetReceivePubkey()
	pubkey := hex.EncodeToString(lockpubkey.SerializeCompressed())

	fmt.Printf("Pay to Public Key (P2PK) lock: %v\n\n", pubkey)
	fmt.Println("You can unlock ecash locked to this public key")

	return nil
}

var mnemonicCmd = &cli.Command{
	Name:   "mnemonic",
	Usage:  "Mnemonic to restore wallet",
	Before: setupWallet,
	Action: mnemonic,
}

func mnemonic(ctx *cli.Context) error {
	mnemonic := nutw.Mnemonic()
	fmt.Printf("mnemonic: %v\n", mnemonic)
	return nil
}

var restoreCmd = &cli.Command{
	Name:   "restore",
	Usage:  "Restore wallet from mnemonic",
	Action: restore,
}

func restore(ctx *cli.Context) error {
	config, err := walletConfig()
	if err != nil {
		printErr(err)
	}
	fmt.Printf("enter mnemonic: ")

	reader := bufio.NewReader(os.Stdin)
	mnemonic, err := reader.ReadString('\n')
	if err != nil {
		log.Fatal("error reading input, please try again")
	}
	mnemonic = mnemonic[:len(mnemonic)-1]

	amountRestored, err := wallet.Restore(config.WalletPath, mnemonic, []string{config.CurrentMintURL})
	if err != nil {
		printErr(fmt.Errorf("error restoring wallet: %v", err))
	}

	fmt.Printf("restored proofs for amount: %v\n", amountRestored)
	return nil
}

var currentMintCmd = &cli.Command{
	Name:  "currentmint",
	Usage: "See and change default mint",
	Subcommands: []*cli.Command{
		{
			Name:      "set",
			Usage:     "Change the current mint",
			ArgsUsage: "[MINT URL]",
			Action:    setCurrentMint,
		},
	},
	Action: currentMint,
}

func currentMint(ctx *cli.Context) error {
	config, err := walletConfig()
	if err != nil {
		printErr(err)
	}
	fmt.Printf("current mint: %v\n", config.CurrentMintURL)
	return nil
}

func setCurrentMint(ctx *cli.Context) error {
	args := ctx.Args()
	if args.Len() < 1 {
		printErr(errors.New("specify new mint url to set as default"))
	}
	mintURL := args.First()
	_, err := url.ParseRequestURI(mintURL)
	if err != nil {
		printErr(fmt.Errorf("invalid mint url: %v", err))
	}

	envFilePath := envPath()
	envFileData, err := os.ReadFile(envFilePath)
	if err != nil {
		printErr(fmt.Errorf("could not read .env file: %v", err))
	}

	lines := strings.Split(string(envFileData), "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "MINT_URL=") {
			// replace line in file setting mint url
			lines[i] = fmt.Sprintf("MINT_URL=%s", mintURL)
		}
	}
	changed := strings.Join(lines, "\n")

	if err := os.WriteFile(envFilePath, []byte(changed), 0644); err != nil {
		printErr(err)
	}

	fmt.Println("updated mint successfully")

	return nil
}

var updateMintCmd = &cli.Command{
	Name:      "update-mint-url",
	ArgsUsage: "[old] [new]",
	Usage:     "Update mint URL",
	Before:    setupWallet,
	Action:    updateMintURL,
}

func updateMintURL(ctx *cli.Context) error {
	if ctx.NArg() != 2 {
		return fmt.Errorf("expected old and new URLs as arguments")
	}
	oldURL := ctx.Args().Get(0)
	newURL := ctx.Args().Get(1)

	_, err := url.ParseRequestURI(newURL)
	if err != nil {
		printErr(fmt.Errorf("invalid URL: %v", err))
	}

	_, err = client.GetMintInfo(newURL)
	if err != nil {
		printErr(fmt.Errorf("new provided URL does not point to a valid mint: %v", err))
	}

	if err := nutw.UpdateMintURL(oldURL, newURL); err != nil {
		printErr(err)
	}
	fmt.Println("Mint URL updated successfully")
	return nil
}

var decodeCmd = &cli.Command{
	Name:      "decode",
	ArgsUsage: "[TOKEN]",
	Usage:     "Decode token",
	Action:    decode,
}

func decode(ctx *cli.Context) error {
	args := ctx.Args()
	if args.Len() < 1 {
		printErr(errors.New("token not provided"))
	}
	serializedToken := args.First()

	token, err := cashu.DecodeToken(serializedToken)
	if err != nil {
		printErr(err)
	}

	jsonToken, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		printErr(err)
	}
	fmt.Printf("token: %s\n", jsonToken)

	return nil
}

func promptMintSelection(action string) string {
	balanceByMints := nutw.GetBalanceByMints()
	mintsLen := len(balanceByMints)

	mints := nutw.TrustedMints()
	slices.Sort(mints)
	selectedMint := nutw.CurrentMint()
	if mintsLen > 1 {
		fmt.Printf("You have balances in %v mints: \n\n", mintsLen)

		for i, mint := range mints {
			balance := balanceByMints[mint]
			fmt.Printf("Mint %v: %v ---- balance: %v sats\n", i+1, mint, balance)
		}

		fmt.Printf("\nSelect from which mint (1-%v) you wish to %v: ", mintsLen, action)

		reader := bufio.NewReader(os.Stdin)
		input, err := reader.ReadString('\n')
		if err != nil {
			log.Fatal("error reading input, please try again")
		}

		num, err := strconv.Atoi(input[:len(input)-1])
		if err != nil {
			printErr(errors.New("invalid number provided"))
		}

		if num <= 0 || num > len(mints) {
			printErr(errors.New("invalid mint selected"))
		}
		selectedMint = mints[num-1]
	}

	return selectedMint
}

func printErr(msg error) {
	fmt.Println(msg.Error())
	os.Exit(0)
}
