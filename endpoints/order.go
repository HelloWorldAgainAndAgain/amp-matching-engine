package endpoints

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/Proofsuite/amp-matching-engine/interfaces"
	"github.com/Proofsuite/amp-matching-engine/utils/httputils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/gorilla/mux"

	"github.com/Proofsuite/amp-matching-engine/types"
	"github.com/Proofsuite/amp-matching-engine/ws"
)

type orderEndpoint struct {
	orderService interfaces.OrderService
	engine       interfaces.Engine
}

// ServeOrderResource sets up the routing of order endpoints and the corresponding handlers.
func ServeOrderResource(
	r *mux.Router,
	orderService interfaces.OrderService,
	engine interfaces.Engine,
) {
	e := &orderEndpoint{orderService, engine}
	r.HandleFunc("/orders/history", e.handleGetOrderHistory).Methods("GET")
	r.HandleFunc("/orders/positions", e.handleGetPositions).Methods("GET")
	r.HandleFunc("/orders", e.handleGetOrders).Methods("GET")
	ws.RegisterChannel(ws.OrderChannel, e.ws)
}

func (e *orderEndpoint) handleGetOrders(w http.ResponseWriter, r *http.Request) {
	v := r.URL.Query()
	addr := v.Get("address")

	if addr == "" {
		httputils.WriteError(w, http.StatusBadRequest, "address Parameter Missing")
		return
	}

	if !common.IsHexAddress(addr) {
		httputils.WriteError(w, http.StatusBadRequest, "Invalid Address")
		return
	}

	address := common.HexToAddress(addr)
	orders, err := e.orderService.GetByUserAddress(address)
	if err != nil {
		logger.Error(err)
		httputils.WriteError(w, http.StatusInternalServerError, "")
		return
	}

	httputils.WriteJSON(w, http.StatusOK, orders)
}

func (e *orderEndpoint) handleGetPositions(w http.ResponseWriter, r *http.Request) {
	v := r.URL.Query()
	addr := v.Get("address")

	if addr == "" {
		httputils.WriteError(w, http.StatusBadRequest, "address Parameter missing")
		return
	}

	if !common.IsHexAddress(addr) {
		httputils.WriteError(w, http.StatusBadRequest, "Invalid Address")
		return
	}

	address := common.HexToAddress(addr)
	orders, err := e.orderService.GetCurrentByUserAddress(address)
	if err != nil {
		logger.Error(err)
		httputils.WriteError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	if orders == nil {
		httputils.WriteJSON(w, http.StatusOK, []types.Order{})
		return
	}

	httputils.WriteJSON(w, http.StatusOK, orders)
}

func (e *orderEndpoint) handleGetOrderHistory(w http.ResponseWriter, r *http.Request) {
	v := r.URL.Query()
	addr := v.Get("address")

	if addr == "" {
		httputils.WriteError(w, http.StatusBadRequest, "address Parameter missing")
		return
	}

	if !common.IsHexAddress(addr) {
		httputils.WriteError(w, http.StatusBadRequest, "Invalid Address")
		return
	}

	address := common.HexToAddress(addr)
	orders, err := e.orderService.GetHistoryByUserAddress(address)
	if err != nil {
		httputils.WriteError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	if orders == nil {
		httputils.WriteJSON(w, http.StatusOK, []types.Order{})
		return
	}

	httputils.WriteJSON(w, http.StatusOK, orders)
}

// ws function handles incoming websocket messages on the order channel
func (e *orderEndpoint) ws(input interface{}, c *ws.Client) {
	msg := &types.WebsocketEvent{}

	bytes, _ := json.Marshal(input)
	if err := json.Unmarshal(bytes, &msg); err != nil {
		logger.Error(err)
		c.SendMessage(ws.OrderChannel, "ERROR", err.Error())
	}

	switch msg.Type {
	case "NEW_ORDER":
		e.handleNewOrder(msg, c)
	case "CANCEL_ORDER":
		e.handleCancelOrder(msg, c)
	case "SUBMIT_SIGNATURE":
		e.handleSubmitSignatures(msg, c)
	default:
		log.Print("Response with error")
	}
}

// handleSubmitSignatures handles NewTrade messages. New trade messages are transmitted to the corresponding order channel
// and received in the handleClientResponse.
func (e *orderEndpoint) handleSubmitSignatures(p *types.WebsocketEvent, c *ws.Client) {
	hash := common.HexToHash(p.Hash)
	ch := e.orderService.GetOrderChannel(hash)
	if ch != nil {
		ch <- p
	}
}

// handleNewOrder handles NewOrder message. New order messages are transmitted to the order service after being unmarshalled
func (e *orderEndpoint) handleNewOrder(ev *types.WebsocketEvent, c *ws.Client) {
	ch := make(chan *types.WebsocketEvent)
	o := &types.Order{}

	bytes, err := json.Marshal(ev.Payload)
	if err != nil {
		logger.Error(err)
		c.SendMessage(ws.OrderChannel, "ERROR", err.Error())
		return
	}

	err = json.Unmarshal(bytes, &o)
	if err != nil {
		logger.Error(err)
		c.SendMessage(ws.OrderChannel, "ERROR", err.Error())
		return
	}

	o.Hash = o.ComputeHash()
	ws.RegisterOrderConnection(o.UserAddress, &ws.OrderConnection{Client: c, ReadChannel: ch})

	err = e.orderService.NewOrder(o)
	if err != nil {
		logger.Error(err)
		c.SendMessage(ws.OrderChannel, "ERROR", err.Error())
		return
	}
}

// handleCancelOrder handles CancelOrder message.
func (e *orderEndpoint) handleCancelOrder(ev *types.WebsocketEvent, c *ws.Client) {
	bytes, err := json.Marshal(ev.Payload)
	oc := &types.OrderCancel{}

	err = oc.UnmarshalJSON(bytes)
	if err != nil {
		logger.Error(err)
		c.SendMessage(ws.OrderChannel, "ERROR", err.Error())
	}

	addr, err := oc.GetSenderAddress()
	if err != nil {
		logger.Error(err)
		c.SendMessage(ws.OrderChannel, "ERROR", err.Error())
	}

	ws.RegisterOrderConnection(addr, &ws.OrderConnection{Client: c})

	err = e.orderService.CancelOrder(oc)
	if err != nil {
		logger.Error(err)
		c.SendMessage(ws.OrderChannel, "ERROR", err.Error())
		return
	}
}
