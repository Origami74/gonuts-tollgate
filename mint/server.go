package mint

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Origami74/gonuts-tollgate/cashu"
	"github.com/Origami74/gonuts-tollgate/cashu/nuts/nut01"
	"github.com/Origami74/gonuts-tollgate/cashu/nuts/nut03"
	"github.com/Origami74/gonuts-tollgate/cashu/nuts/nut04"
	"github.com/Origami74/gonuts-tollgate/cashu/nuts/nut05"
	"github.com/Origami74/gonuts-tollgate/cashu/nuts/nut07"
	"github.com/Origami74/gonuts-tollgate/cashu/nuts/nut09"
	"github.com/gorilla/mux"
)

type ServerConfig struct {
	Port int
	// NOTE: using this value for testing
	MeltTimeout *time.Duration
}

const (
	// 5 minutes
	CACHE_ITEM_TTL    = 60 * 5
	CACHE_ITEMS_LIMIT = 10000
	// 2MB
	REQUEST_BODY_SIZE_LIMIT = 2 * 1024 * 1024

	ACTIVE_KEYSET = "active_keyset_key"
	// 1 day
	KEYSET_TTL = 60 * 60 * 24
)

type CacheItem struct {
	value      []byte
	expiration time.Time
}

type Cache struct {
	items map[string]CacheItem
	mu    sync.RWMutex
	limit int
}

func NewCache() *Cache {
	return &Cache{
		items: make(map[string]CacheItem),
		mu:    sync.RWMutex{},
		limit: CACHE_ITEMS_LIMIT,
	}
}

func (c *Cache) Set(key string, item []byte, expiration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// only set if we are below limit
	if len(c.items) <= c.limit {
		c.items[key] = CacheItem{
			value:      item,
			expiration: time.Now().Add(expiration),
		}
	}
}

func (c *Cache) Get(key string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	item, found := c.items[key]
	if !found {
		return nil, false
	}

	// if it's already expired, it does not hurt to return it still
	// but delete it from cache
	if time.Now().After(item.expiration) {
		delete(c.items, key)
	}

	return item.value, true
}

func (c *Cache) DeleteExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for k, item := range c.items {
		if time.Now().After(item.expiration) {
			delete(c.items, k)
		}
	}
}

type MintServer struct {
	httpServer       *http.Server
	mint             *Mint
	websocketManager *WebsocketManager
	cache            *Cache

	// NOTE: using this value for testing
	meltTimeout *time.Duration
}

func SetupMintServer(m *Mint, config ServerConfig) *MintServer {
	websocketManager := NewWebSocketManager(m)

	mintServer := &MintServer{
		mint:             m,
		websocketManager: websocketManager,
		meltTimeout:      config.MeltTimeout,
		cache:            NewCache(),
	}
	mintServer.setupHttpServer(config.Port)
	return mintServer
}

func (ms *MintServer) Start() error {
	// background goroutine to cleanup cache every 30s
	go func() {
		for {
			select {
			case <-time.Tick(time.Second * 30):
				// check if active keyset has changed and if so, remove from cache
				value, found := ms.cache.Get(ACTIVE_KEYSET)
				if found {
					var activeKeysetCache nut01.GetKeysResponse
					if err := json.Unmarshal(value, &activeKeysetCache); err != nil {
						delete(ms.cache.items, ACTIVE_KEYSET)
						continue
					}

					if len(activeKeysetCache.Keysets) > 0 {
						if ms.mint.activeKeyset.Id != activeKeysetCache.Keysets[0].Id {
							delete(ms.cache.items, ACTIVE_KEYSET)
						}
					}
				}
				// delete any expired items
				ms.cache.DeleteExpired()
			case <-ms.mint.ctx.Done():
				return
			}
		}
	}()

	ms.mint.logger.Info("mint server listening on: " + ms.httpServer.Addr)
	err := ms.httpServer.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		return err
	} else if err == http.ErrServerClosed {
		ms.mint.logger.Info("shutdown complete")
	}
	return nil
}

func (ms *MintServer) Shutdown() error {
	ms.mint.logger.Info("starting shutdown")
	if err := ms.mint.Shutdown(); err != nil {
		return err
	}
	if err := ms.websocketManager.Shutdown(); err != nil {
		return err
	}
	if err := ms.httpServer.Shutdown(context.Background()); err != nil {
		return err
	}
	return nil
}

func (ms *MintServer) setupHttpServer(port int) {
	r := mux.NewRouter()

	r.HandleFunc("/v1/keys", ms.getActiveKeysets).Methods(http.MethodGet, http.MethodOptions)
	r.HandleFunc("/v1/keysets", ms.getKeysetsList).Methods(http.MethodGet, http.MethodOptions)
	r.HandleFunc("/v1/keys/{id}", ms.getKeysetById).Methods(http.MethodGet, http.MethodOptions)
	r.HandleFunc("/v1/mint/quote/{method}", ms.mintRequest).Methods(http.MethodGet, http.MethodPost, http.MethodOptions)
	r.HandleFunc("/v1/mint/quote/{method}/{quote_id}", ms.mintQuoteState).Methods(http.MethodGet, http.MethodPost, http.MethodOptions)
	r.HandleFunc("/v1/mint/{method}", ms.mintTokensRequest).Methods(http.MethodPost, http.MethodOptions)
	r.HandleFunc("/v1/swap", ms.swapRequest).Methods(http.MethodPost, http.MethodOptions)
	r.HandleFunc("/v1/melt/quote/{method}", ms.meltQuoteRequest).Methods(http.MethodPost, http.MethodOptions)
	r.HandleFunc("/v1/melt/quote/{method}/{quote_id}", ms.meltQuoteState).Methods(http.MethodGet, http.MethodOptions)
	r.HandleFunc("/v1/melt/{method}", ms.meltTokens).Methods(http.MethodPost, http.MethodOptions)
	r.HandleFunc("/v1/checkstate", ms.tokenStateCheck).Methods(http.MethodPost, http.MethodOptions)
	r.HandleFunc("/v1/restore", ms.restoreSignatures).Methods(http.MethodPost, http.MethodOptions)
	r.HandleFunc("/v1/info", ms.mintInfo).Methods(http.MethodGet, http.MethodOptions)
	r.HandleFunc("/v1/ws", ms.websocketManager.serveWS).Methods(http.MethodGet, http.MethodOptions)

	r.Use(setupHeaders)

	server := &http.Server{
		Addr:    ":" + strconv.Itoa(port),
		Handler: r,
	}

	ms.httpServer = server
}

func setupHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		rw.Header().Set("Access-Control-Allow-Origin", "*")
		rw.Header().Set("Access-Control-Allow-Credentials", "true")
		rw.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		rw.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, origin")

		if req.Method == http.MethodOptions {
			return
		}

		next.ServeHTTP(rw, req)
	})
}

func (ms *MintServer) logRequest(req *http.Request, statusCode int, format string, args ...any) {
	// this is done to preserve the source position in the log msg from where this
	// method is called. Otherwise all messages would be logged with
	// source line of this log method and not the original caller
	var pcs [1]uintptr
	runtime.Callers(2, pcs[:])
	r := slog.NewRecord(time.Now(), slog.LevelInfo, fmt.Sprintf(format, args...), pcs[0])

	r.Add(slog.Group("request",
		slog.String("method", req.Method),
		slog.String("url", req.URL.String())),
	)
	// add status code attr to log if present
	if statusCode >= 100 {
		r.Add(slog.Int("code", statusCode))
	}
	_ = ms.mint.logger.Handler().Handle(context.Background(), r)
}

// errResponse is the error that will be written in the response
// errLogMsg is the error to log
func (ms *MintServer) writeErr(rw http.ResponseWriter, req *http.Request, errResponse error, errLogMsg ...string) {
	code := http.StatusBadRequest

	log := errResponse.Error()
	// if errLogMsg passed, then log msg different than err response
	if len(errLogMsg) > 0 {
		log = errLogMsg[0]
	}

	var pcs [1]uintptr
	runtime.Callers(2, pcs[:])
	r := slog.NewRecord(time.Now(), slog.LevelError, log, pcs[0])
	r.Add(slog.Group("request",
		slog.String("method", req.Method),
		slog.String("url", req.URL.String())),
		slog.Int("code", code),
	)
	_ = ms.mint.logger.Handler().Handle(context.Background(), r)

	rw.WriteHeader(code)
	errRes, _ := json.Marshal(errResponse)
	rw.Write(errRes)
}

func (ms *MintServer) getActiveKeysets(rw http.ResponseWriter, req *http.Request) {
	activeKeysetResponse, found := ms.cache.Get(ACTIVE_KEYSET)
	if found {
		ms.logRequest(req, http.StatusOK, "returning active keyset from cache")
		rw.Write(activeKeysetResponse)
		return
	}

	activeKeyset := ms.mint.GetActiveKeyset()
	activeKeysets := nut01.GetKeysResponse{Keysets: []nut01.Keyset{activeKeyset}}
	jsonRes, err := json.Marshal(&activeKeysets)
	if err != nil {
		ms.writeErr(rw, req, cashu.StandardErr)
		return
	}

	ms.cache.Set(ACTIVE_KEYSET, jsonRes, time.Second*KEYSET_TTL)

	ms.logRequest(req, http.StatusOK, "returning active keysets")
	rw.Write(jsonRes)
}

func (ms *MintServer) getKeysetsList(rw http.ResponseWriter, req *http.Request) {
	keysetsList := ms.mint.ListKeysets()
	jsonRes, err := json.Marshal(keysetsList)
	if err != nil {
		ms.writeErr(rw, req, cashu.StandardErr)
		return
	}
	ms.logRequest(req, http.StatusOK, "returning list of all keysets")
	rw.Write(jsonRes)
}

func (ms *MintServer) getKeysetById(rw http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	id := vars["id"]

	keysetResponse, found := ms.cache.Get(id)
	if found {
		ms.logRequest(req, http.StatusOK, "returning keyset with id: %v from cache", id)
		rw.Write(keysetResponse)
		return
	}

	keyset, err := ms.mint.GetKeysetById(id)
	if err != nil {
		ms.writeErr(rw, req, cashu.UnknownKeysetErr)
		return
	}
	keysets := nut01.GetKeysResponse{Keysets: []nut01.Keyset{keyset}}
	jsonRes, err := json.Marshal(&keysets)
	if err != nil {
		ms.writeErr(rw, req, cashu.StandardErr)
		return
	}

	ms.cache.Set(id, jsonRes, time.Second*KEYSET_TTL)

	ms.logRequest(req, http.StatusOK, "returning keyset with id: %v", id)
	rw.Write(jsonRes)
}

func (ms *MintServer) mintRequest(rw http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	method := vars["method"]
	if method != cashu.BOLT11_METHOD {
		ms.writeErr(rw, req, cashu.PaymentMethodNotSupportedErr)
		return
	}

	var mintReq nut04.PostMintQuoteBolt11Request
	err := decodeJsonReqBody(req, &mintReq)
	if err != nil {
		ms.writeErr(rw, req, err)
		return
	}

	ms.logRequest(req, 0, "mint request for %v %v", mintReq.Amount, mintReq.Unit)
	mintQuote, err := ms.mint.RequestMintQuote(mintReq)
	if err != nil {
		cashuErr, ok := err.(*cashu.Error)
		// note: if there was internal error from lightning backend generating invoice
		// or error from db, log that error but return generic response
		if ok {
			if cashuErr.Code == cashu.LightningBackendErrCode || cashuErr.Code == cashu.DBErrCode {
				ms.writeErr(rw, req, cashu.StandardErr, cashuErr.Error())
				return
			}
		}
		ms.writeErr(rw, req, err)
		return
	}

	mintQuoteResponse := nut04.PostMintQuoteBolt11Response{
		Quote:   mintQuote.Id,
		Request: mintQuote.PaymentRequest,
		Amount:  mintQuote.Amount,
		Unit:    cashu.Sat.String(),
		State:   mintQuote.State,
		Expiry:  mintQuote.Expiry,
	}
	if mintQuote.Pubkey != nil {
		mintQuoteResponse.Pubkey = hex.EncodeToString(mintQuote.Pubkey.SerializeCompressed())
	}

	jsonRes, err := json.Marshal(&mintQuoteResponse)
	if err != nil {
		ms.writeErr(rw, req, cashu.StandardErr)
		return
	}

	ms.logRequest(req, http.StatusOK, "created mint quote %v", mintQuote.Id)
	rw.Write(jsonRes)
}

func (ms *MintServer) mintQuoteState(rw http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	method := vars["method"]
	if method != cashu.BOLT11_METHOD {
		ms.writeErr(rw, req, cashu.PaymentMethodNotSupportedErr)
		return
	}

	quoteId := vars["quote_id"]
	mintQuote, err := ms.mint.GetMintQuoteState(quoteId)
	if err != nil {
		cashuErr, ok := err.(*cashu.Error)
		// note: if there was internal error from lightning backend
		// or error from db, log that error but return generic response
		if ok {
			if cashuErr.Code == cashu.LightningBackendErrCode || cashuErr.Code == cashu.DBErrCode {
				ms.writeErr(rw, req, cashu.StandardErr, cashuErr.Error())
				return
			}
		}

		ms.writeErr(rw, req, err)
		return
	}

	mintQuoteStateResponse := nut04.PostMintQuoteBolt11Response{
		Quote:   mintQuote.Id,
		Request: mintQuote.PaymentRequest,
		Amount:  mintQuote.Amount,
		Unit:    cashu.Sat.String(),
		State:   mintQuote.State,
		Expiry:  mintQuote.Expiry,
	}
	if mintQuote.Pubkey != nil {
		mintQuoteStateResponse.Pubkey = hex.EncodeToString(mintQuote.Pubkey.SerializeCompressed())
	}

	jsonRes, err := json.Marshal(&mintQuoteStateResponse)
	if err != nil {
		ms.writeErr(rw, req, cashu.StandardErr)
		return
	}

	ms.logRequest(req, http.StatusOK, "returning mint quote with state '%s'", mintQuote.State)
	rw.Write(jsonRes)
}

func (ms *MintServer) mintTokensRequest(rw http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	method := vars["method"]
	if method != cashu.BOLT11_METHOD {
		ms.writeErr(rw, req, cashu.PaymentMethodNotSupportedErr)
		return
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		ms.writeErr(rw, req, cashu.StandardErr)
		return
	}

	req.Body = io.NopCloser(bytes.NewReader(body))
	var mintReq nut04.PostMintBolt11Request
	if err := decodeJsonReqBody(req, &mintReq); err != nil {
		ms.writeErr(rw, req, err)
		return
	}

	// check in cache first. Look at: https://github.com/cashubtc/nuts/blob/main/19.md
	response, found := ms.cache.Get(req.Method + req.URL.String() + string(body))
	if found {
		ms.mint.logDebugf("returning signatures for mint quote '%v' from cache", mintReq.Quote)
		ms.logRequest(req, http.StatusOK, "returning signatures on mint tokens request")
		rw.Write(response)
		return
	}

	blindedSignatures, err := ms.mint.MintTokens(mintReq)
	if err != nil {
		cashuErr, ok := err.(*cashu.Error)
		// note: if there was internal error from lightning backend
		// or error from db, log that error but return generic response
		if ok {
			if cashuErr.Code == cashu.LightningBackendErrCode || cashuErr.Code == cashu.DBErrCode {
				ms.writeErr(rw, req, cashu.StandardErr, cashuErr.Error())
				return
			}
		}

		ms.writeErr(rw, req, err)
		return
	}

	signatures := nut04.PostMintBolt11Response{Signatures: blindedSignatures}
	jsonRes, err := json.Marshal(&signatures)
	if err != nil {
		ms.writeErr(rw, req, cashu.StandardErr)
		return
	}

	// if less than 2MB, write request/response pair to cache
	if len(body) < REQUEST_BODY_SIZE_LIMIT {
		ms.cache.Set(req.Method+req.URL.String()+string(body), jsonRes, time.Second*CACHE_ITEM_TTL)
	}

	ms.logRequest(req, http.StatusOK, "returning signatures on mint tokens request")
	rw.Write(jsonRes)
}

func (ms *MintServer) swapRequest(rw http.ResponseWriter, req *http.Request) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		ms.writeErr(rw, req, cashu.StandardErr)
		return
	}

	req.Body = io.NopCloser(bytes.NewReader(body))
	var swapReq nut03.PostSwapRequest
	if err := decodeJsonReqBody(req, &swapReq); err != nil {
		ms.writeErr(rw, req, err)
		return
	}

	// check in cache first. Look at: https://github.com/cashubtc/nuts/blob/main/19.md
	response, found := ms.cache.Get(req.Method + req.URL.String() + string(body))
	if found {
		ms.mint.logDebugf("returning signatures for swap request from cache")
		ms.logRequest(req, http.StatusOK, "returning signatures on swap request")
		rw.Write(response)
		return
	}

	blindedSignatures, err := ms.mint.Swap(swapReq.Inputs, swapReq.Outputs)
	if err != nil {
		cashuErr, ok := err.(*cashu.Error)
		// note: if there was internal error from db
		// log that error but return generic response
		if ok && cashuErr.Code == cashu.DBErrCode {
			ms.writeErr(rw, req, cashu.StandardErr, cashuErr.Error())
			return
		}

		ms.writeErr(rw, req, err)
		return
	}

	signatures := nut03.PostSwapResponse{Signatures: blindedSignatures}
	jsonRes, err := json.Marshal(&signatures)
	if err != nil {
		ms.writeErr(rw, req, cashu.StandardErr)
		return
	}

	// if less than 2MB, write request/response pair to cache
	if len(body) < REQUEST_BODY_SIZE_LIMIT {
		ms.cache.Set(req.Method+req.URL.String()+string(body), jsonRes, time.Second*CACHE_ITEM_TTL)
	}

	ms.logRequest(req, http.StatusOK, "returning signatures on swap request")
	rw.Write(jsonRes)
}

func (ms *MintServer) meltQuoteRequest(rw http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	method := vars["method"]
	if method != cashu.BOLT11_METHOD {
		ms.writeErr(rw, req, cashu.PaymentMethodNotSupportedErr)
		return
	}

	var meltRequest nut05.PostMeltQuoteBolt11Request
	err := decodeJsonReqBody(req, &meltRequest)
	if err != nil {
		ms.writeErr(rw, req, err)
		return
	}

	meltQuote, err := ms.mint.RequestMeltQuote(meltRequest)
	if err != nil {
		cashuErr, ok := err.(*cashu.Error)
		// note: if there was internal error from db
		// log that error but return generic response
		if ok && cashuErr.Code == cashu.DBErrCode {
			ms.writeErr(rw, req, cashu.StandardErr, cashuErr.Error())
			return
		}
		ms.writeErr(rw, req, err)
		return
	}

	meltQuoteResponse := &nut05.PostMeltQuoteBolt11Response{
		Quote:      meltQuote.Id,
		Request:    meltQuote.InvoiceRequest,
		Amount:     meltQuote.Amount,
		Unit:       cashu.Sat.String(),
		FeeReserve: meltQuote.FeeReserve,
		State:      meltQuote.State,
		Expiry:     meltQuote.Expiry,
	}

	jsonRes, err := json.Marshal(&meltQuoteResponse)
	if err != nil {
		ms.writeErr(rw, req, cashu.StandardErr)
		return
	}

	ms.logRequest(req, http.StatusOK,
		"returning melt quote '%v' for invoice with payment hash: %v", meltQuote.Id, meltQuote.PaymentHash)

	rw.Write(jsonRes)
}

func (ms *MintServer) meltQuoteState(rw http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	method := vars["method"]
	if method != cashu.BOLT11_METHOD {
		ms.writeErr(rw, req, cashu.PaymentMethodNotSupportedErr)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	quoteId := vars["quote_id"]
	meltQuote, err := ms.mint.GetMeltQuoteState(ctx, quoteId)
	if err != nil {
		cashuErr, ok := err.(*cashu.Error)
		// note: if there was internal error from lightning backend
		// or error from db, log that error but return generic response
		if ok {
			if cashuErr.Code == cashu.LightningBackendErrCode || cashuErr.Code == cashu.DBErrCode {
				ms.writeErr(rw, req, cashu.StandardErr, cashuErr.Error())
				return
			}
		}
		ms.writeErr(rw, req, err)
		return
	}

	quoteState := &nut05.PostMeltQuoteBolt11Response{
		Quote:      meltQuote.Id,
		Request:    meltQuote.InvoiceRequest,
		Amount:     meltQuote.Amount,
		Unit:       cashu.Sat.String(),
		FeeReserve: meltQuote.FeeReserve,
		State:      meltQuote.State,
		Expiry:     meltQuote.Expiry,
		Preimage:   meltQuote.Preimage,
	}

	jsonRes, err := json.Marshal(&quoteState)
	if err != nil {
		ms.writeErr(rw, req, cashu.StandardErr)
		return
	}

	ms.logRequest(req, http.StatusOK, "returning melt quote with state '%s'", meltQuote.State)
	rw.Write(jsonRes)
}

func (ms *MintServer) meltTokens(rw http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	method := vars["method"]
	if method != cashu.BOLT11_METHOD {
		ms.writeErr(rw, req, cashu.PaymentMethodNotSupportedErr)
		return
	}

	var meltTokensRequest nut05.PostMeltBolt11Request
	err := decodeJsonReqBody(req, &meltTokensRequest)
	if err != nil {
		ms.writeErr(rw, req, err)
		return
	}

	timeout := time.Minute * 1
	if ms.meltTimeout != nil {
		timeout = *ms.meltTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	meltQuote, err := ms.mint.MeltTokens(ctx, meltTokensRequest)
	if err != nil {
		cashuErr, ok := err.(*cashu.Error)
		// note: if there was internal error from lightning backend
		// or error from db, log that error but return generic response
		if ok {
			if cashuErr.Code == cashu.LightningBackendErrCode {
				responseError := cashu.BuildCashuError("unable to send payment", cashu.StandardErrCode)
				ms.writeErr(rw, req, responseError, cashuErr.Error())
				return
			} else if cashuErr.Code == cashu.DBErrCode {
				ms.writeErr(rw, req, cashu.StandardErr, cashuErr.Error())
				return
			}
		}
		ms.writeErr(rw, req, err)
		return
	}

	meltQuoteResponse := &nut05.PostMeltQuoteBolt11Response{
		Quote:      meltQuote.Id,
		Request:    meltQuote.InvoiceRequest,
		Amount:     meltQuote.Amount,
		Unit:       cashu.Sat.String(),
		FeeReserve: meltQuote.FeeReserve,
		State:      meltQuote.State,
		Expiry:     meltQuote.Expiry,
		Preimage:   meltQuote.Preimage,
	}

	jsonRes, err := json.Marshal(&meltQuoteResponse)
	if err != nil {
		ms.writeErr(rw, req, cashu.StandardErr)
		return
	}

	ms.logRequest(req, http.StatusOK,
		"return from melt tokens for quote '%v'. Quote state: %s", meltQuote.Id, meltQuote.State)

	rw.Write(jsonRes)
}

func (ms *MintServer) tokenStateCheck(rw http.ResponseWriter, req *http.Request) {
	var stateRequest nut07.PostCheckStateRequest
	err := decodeJsonReqBody(req, &stateRequest)
	if err != nil {
		ms.writeErr(rw, req, err)
		return
	}

	proofStates, err := ms.mint.ProofsStateCheck(stateRequest.Ys)
	if err != nil {
		cashuErr, ok := err.(*cashu.Error)
		// note: if there was internal error from lightning backend
		// or error from db, log that error but return generic response
		if ok {
			if cashuErr.Code == cashu.LightningBackendErrCode || cashuErr.Code == cashu.DBErrCode {
				ms.writeErr(rw, req, cashu.StandardErr, cashuErr.Error())
				return
			}
		}
		ms.writeErr(rw, req, err)
		return
	}

	checkStateResponse := nut07.PostCheckStateResponse{States: proofStates}
	jsonRes, err := json.Marshal(&checkStateResponse)
	if err != nil {
		ms.writeErr(rw, req, cashu.StandardErr)
		return
	}

	ms.logRequest(req, http.StatusOK, "returning proof states")
	rw.Write(jsonRes)
}

func (ms *MintServer) restoreSignatures(rw http.ResponseWriter, req *http.Request) {
	var restoreRequest nut09.PostRestoreRequest
	err := decodeJsonReqBody(req, &restoreRequest)
	if err != nil {
		ms.writeErr(rw, req, err)
		return
	}

	blindedMessages, blindedSignatures, err := ms.mint.RestoreSignatures(restoreRequest.Outputs)
	if err != nil {
		ms.writeErr(rw, req, cashu.StandardErr, err.Error())
		return
	}

	restoreResponse := nut09.PostRestoreResponse{
		Outputs:    blindedMessages,
		Signatures: blindedSignatures,
	}
	jsonRes, err := json.Marshal(&restoreResponse)
	if err != nil {
		ms.writeErr(rw, req, cashu.StandardErr)
		return
	}

	ms.logRequest(req, http.StatusOK, "returning signatures from restore request")
	rw.Write(jsonRes)
}

func (ms *MintServer) mintInfo(rw http.ResponseWriter, req *http.Request) {
	mintInfo, err := ms.mint.RetrieveMintInfo()
	if err != nil {
		ms.writeErr(rw, req, cashu.StandardErr, err.Error())
		return
	}

	jsonRes, err := json.Marshal(&mintInfo)
	if err != nil {
		ms.writeErr(rw, req, cashu.StandardErr)
		return
	}

	ms.logRequest(req, http.StatusOK, "returning mint info")
	rw.Write(jsonRes)
}

func decodeJsonReqBody(req *http.Request, dst any) error {
	ct := req.Header.Get("Content-Type")
	if ct != "" {
		mediaType := strings.ToLower(strings.Split(ct, ";")[0])
		if mediaType != "application/json" {
			ctError := cashu.BuildCashuError("Content-Type header is not application/json", cashu.StandardErrCode)
			return ctError
		}
	}

	dec := json.NewDecoder(req.Body)

	err := dec.Decode(&dst)
	if err != nil {
		var syntaxErr *json.SyntaxError
		var typeErr *json.UnmarshalTypeError
		var cashuErr *cashu.Error

		switch {
		case errors.As(err, &syntaxErr):
			msg := fmt.Sprintf("bad json at %d", syntaxErr.Offset)
			cashuErr = cashu.BuildCashuError(msg, cashu.StandardErrCode)

		case errors.As(err, &typeErr):
			msg := fmt.Sprintf("invalid %v for field %q", typeErr.Value, typeErr.Field)
			cashuErr = cashu.BuildCashuError(msg, cashu.StandardErrCode)

		case errors.Is(err, io.EOF):
			return cashu.EmptyBodyErr

		default:
			cashuErr = cashu.BuildCashuError(err.Error(), cashu.StandardErrCode)
		}
		return cashuErr
	}

	return nil
}
