package submanager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/Origami74/gonuts-tollgate/cashu"
	"github.com/Origami74/gonuts-tollgate/cashu/nuts/nut17"
	"github.com/Origami74/gonuts-tollgate/wallet/client"
	"github.com/gorilla/websocket"
)

var (
	ErrNUT17NotSupported = errors.New("NUT-17 Not supported")
)

type SubscriptionManager struct {
	wsConn *websocket.Conn
	mu     sync.RWMutex
	// aggregate writes through this channel since there can only be one concurrent writer.
	send chan json.RawMessage

	subs             map[string]*Subscription
	idCounter        int
	supportedMethods []nut17.SupportedMethod

	ctx    context.Context
	cancel context.CancelFunc
}

func NewSubscriptionManager(mint string) (*SubscriptionManager, error) {
	mintInfo, err := client.GetMintInfo(mint)
	if err != nil {
		return nil, fmt.Errorf("could not get mint info: %v", err)
	}
	if len(mintInfo.Nuts.Nut17.Supported) == 0 {
		return nil, ErrNUT17NotSupported
	}

	mintURL, err := url.Parse(mint)
	if err != nil {
		return nil, fmt.Errorf("invalid mint url: %v", err)
	}

	scheme := "ws"
	if mintURL.Scheme == "https" {
		scheme = "wss"
	}
	wsURL := scheme + "://" + mintURL.Host + mintURL.Path + "/v1/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	subManager := &SubscriptionManager{
		wsConn:           conn,
		send:             make(chan json.RawMessage),
		subs:             make(map[string]*Subscription),
		idCounter:        0,
		supportedMethods: mintInfo.Nuts.Nut17.Supported,
		ctx:              ctx,
		cancel:           cancel,
	}

	return subManager, nil
}

// Run should be called in a separate goroutine to run in the background.
// It will be stopped when Close is called on the SubscriptionManager
func (sm *SubscriptionManager) Run(errChan chan<- error) {
	wsMessages := make(chan []byte)

	go func() {
		for {
			_, msg, err := sm.wsConn.ReadMessage()
			if err != nil {
				errChan <- err
				return
			}
			wsMessages <- msg
		}
	}()

	// write messages on the connection in this goroutine
	go func() {
		for {
			select {
			case msgToSend := <-sm.send:
				sm.wsConn.WriteMessage(websocket.TextMessage, msgToSend)
			case <-sm.ctx.Done():
				return
			}
		}
	}()

	for {
		select {
		case msg, ok := <-wsMessages:
			if !ok {
				return
			}

			var notification nut17.WsNotification
			if err := json.Unmarshal(msg, &notification); err == nil {
				subId := notification.Params.SubId
				// if subscription exists, send notification on that channel
				sub, ok := sm.subs[subId]
				if ok {
					sub.notificationChannel <- notification
					continue
				}
			}

			// if could not parse as WsNotification, try parsing as WsResponse
			var response nut17.WsResponse
			if err := json.Unmarshal(msg, &response); err != nil {
				// if could not parse as WsResponse, try parsing as WsError
				var wsError nut17.WsError
				if err := json.Unmarshal(msg, &wsError); err == nil {
					// if WsError, check if there is subscription with that id and send on err channel
					for _, subscription := range sm.subs {
						if subscription.id == wsError.Id {
							subscription.errChannel <- wsError
							continue
						}
					}
				}
			}

			// if WsResponse, check if there is subscription with that id and send on response channel
			for _, subscription := range sm.subs {
				if subscription.id == response.Id {
					subscription.responseChannel <- response
					continue
				}
			}
		case <-sm.ctx.Done():
			return
		}
	}
}

func (sm *SubscriptionManager) removeSubscription(id string) {
	sm.mu.Lock()
	delete(sm.subs, id)
	sm.mu.Unlock()
}

func (sm *SubscriptionManager) Subscribe(kind nut17.SubscriptionKind, filters []string) (*Subscription, error) {
	if len(filters) < 1 {
		return nil, errors.New("filters cannot be empty")
	}

	if !sm.IsSubscriptionKindSupported(kind) {
		return nil, fmt.Errorf("subscription to %s not supported by mint", kind)
	}

	id := sm.idCounter
	hash := sha256.Sum256([]byte(filters[0]))
	subId := hex.EncodeToString(hash[:])

	// send subscription request message
	request := nut17.WsRequest{
		JsonRPC: "2.0",
		Method:  "subscribe",
		Params: nut17.RequestParams{
			Kind:    kind.String(),
			SubId:   subId,
			Filters: filters,
		},
		Id: id,
	}

	jsonRequest, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("could not make subscription request: %v", err)
	}
	sm.send <- jsonRequest

	sub := &Subscription{
		id:                  id,
		subId:               subId,
		responseChannel:     make(chan nut17.WsResponse),
		notificationChannel: make(chan nut17.WsNotification),
		errChannel:          make(chan nut17.WsError),
	}

	// increment id counter and add subscription
	sm.mu.Lock()
	sm.idCounter++
	sm.subs[subId] = sub
	sm.mu.Unlock()

	select {
	case response := <-sub.responseChannel:
		if response.Result.Status == "OK" {
			return sub, nil
		}
	case err := <-sub.errChannel:
		sm.removeSubscription(subId)
		return nil, fmt.Errorf("could not setup subscription to mint: %v", err.Error())
	case <-time.After(10 * time.Second):
		// remove sub from sub list if did not receive anything after 10 seconds
		// and return error
		sm.removeSubscription(subId)
		return nil, errors.New("could not setup subscription to mint")
	}

	sm.removeSubscription(subId)
	return nil, errors.New("could not setup subscription to mint")
}

func (sm *SubscriptionManager) CloseSubscripton(subId string) error {
	_, ok := sm.subs[subId]
	if !ok {
		return errors.New("subscription does not exist")
	}

	// send request to unsubscribe
	request := nut17.WsRequest{
		JsonRPC: "2.0",
		Method:  "unsubscribe",
		Params: nut17.RequestParams{
			SubId: subId,
		},
		Id: sm.idCounter,
	}

	// increment id counter
	sm.mu.Lock()
	sm.idCounter++
	sm.mu.Unlock()

	jsonRequest, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("could not make unsubscription request: %v", err)
	}
	sm.send <- jsonRequest
	sm.removeSubscription(subId)

	return nil
}

func (sm *SubscriptionManager) IsSubscriptionKindSupported(kind nut17.SubscriptionKind) bool {
	for _, method := range sm.supportedMethods {
		if method.Method == cashu.BOLT11_METHOD {
			if slices.Contains(method.Commands, kind.String()) {
				return true
			}
		}
	}
	return false
}

func (sm *SubscriptionManager) Close() error {
	sm.cancel()
	if err := sm.wsConn.Close(); err != nil {
		return err
	}
	return nil
}

type Subscription struct {
	subId               string
	id                  int
	responseChannel     chan nut17.WsResponse
	notificationChannel chan nut17.WsNotification
	errChannel          chan nut17.WsError
}

func (s *Subscription) Read() (nut17.WsNotification, error) {
	select {
	case msg, ok := <-s.notificationChannel:
		if ok {
			return msg, nil
		} else {
			return nut17.WsNotification{}, errors.New("could not read from subscription. Channel got closed")
		}
	}
}

func (s *Subscription) SubId() string {
	return s.subId
}
