package jsclient

import (
	"encoding/json"
	"errors"
	"github.com/768bit/gopherws/websocketjs"
	"github.com/768bit/vutils"
	"github.com/gopherjs/gopherjs/js"
	"github.com/gopherjs/jsbuiltin"
	"gitlab.768bit.com/vann/libvann/vannws_utils"
	"log"
	"net/url"
)

var default_loc = js.Global.Get("document").Get("location")
var default_host = default_loc.Get("host").String()
var default_proto = default_loc.Get("protocol").String()

type WebSocketClient struct {
	conn        *websocketjs.WebSocket
	host        string
	scheme      string
	path        string
	reqStack    map[string]*vannws_utils.WSRequest
	reqQueue    []*vannws_utils.WSRequest
	ReadyChan   chan bool
	doneChan    chan bool
	errChan     chan error
	ready       bool
	connected   bool
	authorised  bool
	session     *vannws_utils.WSClientSession
	jwtTicketID string
	userUUID    string
	isBasic     bool
}

func DefaultWebSocketClient(path string) *WebSocketClient {

	var scheme = "ws"

	if default_proto == "https:" {

		scheme = "wss"

	}

	return NewWebSocketClient(scheme, default_host, path)

}

func NewWebSocketClient(scheme string, host string, path string) *WebSocketClient {

	wsc := &WebSocketClient{
		scheme:     scheme,
		host:       host,
		path:       path,
		reqStack:   map[string]*vannws_utils.WSRequest{},
		ReadyChan:  make(chan bool),
		ready:      false,
		connected:  false,
		reqQueue:   []*vannws_utils.WSRequest{},
		authorised: false,
		isBasic:    false,
	}

	return wsc

}

func (wsc *WebSocketClient) ConnectWS(jwtTicketID string, userUUID string, doneChan chan bool, errChan chan error) {

	println("Starting Websocket Connection")
	wsc.jwtTicketID = jwtTicketID
	u := url.URL{Scheme: wsc.scheme, Host: wsc.host, Path: wsc.path}
	log.Printf("connecting to %s", u.String())
	wsc.doneChan = doneChan
	wsc.errChan = errChan
	wsc.userUUID = userUUID

	ws, err := websocketjs.New(u.String())
	if err != nil {
		errChan <- err
		close(doneChan)
		close(errChan)
		return
	}
	onOpen := func(ev *js.Object) {
		wsc.connected = true
	}
	onMessage := func(ev *js.Object) {
		//lets ascertain what the message type is...
		data := ev.Get("data")
		to := jsbuiltin.TypeOf(data)
		abi := jsbuiltin.InstanceOf(data, js.Global.Get("ArrayBuffer"))
		bli := jsbuiltin.InstanceOf(data, js.Global.Get("Blob"))
		if abi == true {
			//process the ArrayBuffer method
			println("ArrayBuffer")
		} else if bli == true {
			//process via Blob
			println("Blob")
		} else if to == "string" {
			println("string")
			go wsc.handleReceiveJSON(ws, []byte(data.String()))
		}
	}
	onClose := func(ev *js.Object) {
		wsc.connected = false
		go func() {
			doneChan <- true
		}()
	}
	onError := func(err *js.Object) {
		println(err.Interface())
		go func() {
			errChan <- errors.New("Error in WebSocket Client Connection")
		}()
	}
	ws.AddEventListener("open", false, onOpen)
	ws.AddEventListener("message", false, onMessage)
	ws.AddEventListener("close", false, onClose)
	ws.AddEventListener("error", false, onError)
	wsc.conn = ws

}

func (wsc *WebSocketClient) ConnectBasicWS(doneChan chan bool, errChan chan error) {

	println("Starting Basic Websocket Connection")
	u := url.URL{Scheme: wsc.scheme, Host: wsc.host, Path: wsc.path}
	log.Printf("connecting to %s", u.String())
	wsc.doneChan = doneChan
	wsc.errChan = errChan
	wsc.isBasic = true

	ws, err := websocketjs.New(u.String())
	if err != nil {
		go func() {
			errChan <- err
			close(doneChan)
			close(errChan)
			close(wsc.ReadyChan)
		}()
		return
	}
	onOpen := func(ev *js.Object) {
		wsc.connected = true
		go func() {
			wsc.ready = true
			wsc.ReadyChan <- true
		}()
	}
	onMessage := func(ev *js.Object) {
		//lets ascertain what the message type is...
		data := ev.Get("data")
		to := jsbuiltin.TypeOf(data)
		abi := jsbuiltin.InstanceOf(data, js.Global.Get("ArrayBuffer"))
		bli := jsbuiltin.InstanceOf(data, js.Global.Get("Blob"))
		if abi == true {
			//process the ArrayBuffer method
			println("ArrayBuffer")
		} else if bli == true {
			//process via Blob
			println("Blob")
		} else if to == "string" {
			println("string")
			go wsc.handleReceiveJSON(ws, []byte(data.String()))
		}
	}
	onClose := func(ev *js.Object) {
		wsc.connected = false
		go func() {
			wsc.ReadyChan <- false
			errChan <- errors.New("WebSocket Client Connection Closed")
			defer close(doneChan)
			defer close(errChan)
			defer close(wsc.ReadyChan)
			doneChan <- true
		}()
	}
	onError := func(err *js.Object) {
		println(err.Interface())
		go func() {
			wsc.ReadyChan <- false
			doneChan <- false
			//defer close(doneChan)
			//defer close(errChan)
			//defer close(wsc.ReadyChan)
			errChan <- errors.New("Error in WebSocket Client Connection")
		}()
	}
	ws.AddEventListener("open", false, onOpen)
	ws.AddEventListener("message", false, onMessage)
	ws.AddEventListener("close", false, onClose)
	ws.AddEventListener("error", false, onError)
	wsc.conn = ws

}

func (wsc *WebSocketClient) handleReceiveJSON(conn *websocketjs.WebSocket, data []byte) {

	//need to lookup the request in the stack signal done then pass the response... if the response is an error that can be handled too...

	//this could be any type of message and is a low level handler that will coerce the available types and make it

	//need to figure out what the basic parameters are (message type, request id etc..)

	//coerce to the vannws_utils.WebSocketResponseBody type

	var responseBody vannws_utils.WebSocketResponseBody

	if err := json.Unmarshal(data, &responseBody); err != nil {

		//error marshalling data from JSON...

		println("Error unmarshalling", err)

	} else if wsc.isBasic {

		//in basic connection scenarios we just need to pass messages backwards and forwards... these are plain objects...

		if wsc.reqStack[responseBody.ID] != nil {

			wsc.reqStack[responseBody.ID].Done <- true
			wsc.reqStack[responseBody.ID].Response <- &responseBody
			delete(wsc.reqStack, responseBody.ID)

		}

	} else {

		//content is fine figure out what it is...

		switch responseBody.MessageType {

		case vannws_utils.ServerHelloMessage:
			wsc.connected = true
			wsc.ready = true
			//immediately negotiate a session so we can ready the client

			wsc.newWSSession() //send the ready signal to the channel - we can start processing requests...
		default:
			//try a lookup to see if anythign is in the request stack
			if wsc.reqStack[responseBody.ID] != nil {

				wsc.reqStack[responseBody.ID].Done <- true
				wsc.reqStack[responseBody.ID].Response <- &responseBody
				delete(wsc.reqStack, responseBody.ID)

			}

		}

	}

}

func (wsc *WebSocketClient) processJSONResponse(conn *websocketjs.WebSocket, response *vannws_utils.WebSocketResponseBody) {

	//need to verify the session etc...

}

func (wsc *WebSocketClient) SendRpcRequest(modURI string, cmd string, path string, payload map[string]interface{},
	options map[string]interface{}, headers map[string]string) (*vannws_utils.WSRequest, error) {

	//generate an ID for the request

	if !wsc.connected {

		//attempt reconnect? add to queue?

		return nil, errors.New("WebSocket Client not Connected")

	}

	if !wsc.ready {

		return nil, errors.New("WebSocket Connection Not Ready Yet. Awaiting Response from Server.")

	}

	reqID, err := vutils.UUID.MakeUUIDStringNoDashes()

	if err != nil {

		return nil, err

	}

	reqBody := vannws_utils.WebSocketRequestBody{
		MessageType: vannws_utils.RPCMessage,
		Cmd:         cmd,
		ID:          reqID,
		SeshKey:     wsc.conn.GetSeshKey(),
		ModuleURI:   modURI,
		Path:        path,
		Options:     options,
		Payload:     payload,
		Headers:     headers,
	}

	//create the item on the request stack...

	wsc.reqStack[reqID] = vannws_utils.NewWSRequest(reqID, wsc.conn.GetSeshKey(), &reqBody)

	go SendJSONRequest(reqID, wsc.conn, reqBody, wsc.reqStack[reqID])

	return wsc.reqStack[reqID], nil

}

func (wsc *WebSocketClient) SendHttpRequest(method string, path string, payload map[string]interface{},
	options map[string]interface{}, headers map[string]string) (*vannws_utils.WSRequest, error) {

	if !wsc.connected {

		//attempt reconnect?

		return nil, errors.New("WebSocket Client not Connected")

	}

	if !wsc.ready {

		return nil, errors.New("WebSocket Connection Not Ready Yet. Awaiting Response from Server.")

	}

	reqID, err := vutils.UUID.MakeUUIDStringNoDashes()

	if err != nil {

		return nil, err

	}

	reqBody := vannws_utils.WebSocketRequestBody{
		MessageType: vannws_utils.HTTPMessage,
		Method:      method,
		ID:          reqID,
		SeshKey:     wsc.conn.GetSeshKey(),
		Path:        path,
		Options:     options,
		Payload:     payload,
		Headers:     headers,
	}

	//create the item on the request stack...

	wsc.reqStack[reqID] = vannws_utils.NewWSHttpRequest(reqID, wsc.conn.GetSeshKey(), &reqBody)

	go SendJSONRequest(reqID, wsc.conn, reqBody, wsc.reqStack[reqID])

	return wsc.reqStack[reqID], nil

}

func (wsc *WebSocketClient) closeWS() {

	//close the session on the server then terminate the connection...
	//err := wsc.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	//if err != nil {
	//log.Println("write close:", err)
	//return
	//}

}

//now for handling json payloads...

func (wsc *WebSocketClient) newWSSession() {

	//make the new WSSession on *this* server and maintain it until cleaned up...

	reqID, err := vutils.UUID.MakeUUIDStringNoDashes()

	if err != nil {

		return

	}

	reqBody := vannws_utils.NewWebSocketSessionStartRequestBody(reqID, wsc.jwtTicketID, wsc.userUUID)

	req := vannws_utils.NewWSRequestWithTimeout(reqID, wsc.conn.GetSeshKey(), reqBody, 15)

	wsc.reqStack[reqID] = req

	go SendJSONRequest(reqID, wsc.conn, reqBody, wsc.reqStack[reqID])

	for {
		select {
		case <-req.Progress:
			//handle the progress...
		case success := <-req.Done:
			//reqest completed was it successful?
			if !success {

			}
		case resp := <-req.Response:
			if resp == nil {

			} else if req.Cancelled == true {

			} else if resp.MessageType == vannws_utils.RPCSessionStartMessage && resp.StatusCode == vannws_utils.RPCStatusOK {
				//handle the response as required...
				//get the seshKey
				println("Successfully started session with server:", resp.SeshKey)
				wsc.conn.SetSeshKey(resp.SeshKey)
				wsc.ReadyChan <- true
			} else if resp.MessageType == vannws_utils.RPCSessionStartErrorMessage {
				//handle the response as required...
				wsc.ReadyChan <- false
			} else {
				wsc.ReadyChan <- false
				//bad things have happened!

			}
		}
	}

}

func (ws *WebSocketClient) closeWSSession(id uint64) {

	//make the new WSSession on *this* server and maintain it until cleaned up...

}

func (ws *WebSocketClient) newWSStreamSession() {

	//the client intends to start a stream session with the server

}

func (ws *WebSocketClient) closeWSStreamSession(conn *websocketjs.WebSocket) {

	//the server will have responded in kind to the close request or we are cleaning up the sessions client side....

}

func (wsc *WebSocketClient) SendBasicRequest(payload map[string]interface{}) (*vannws_utils.WSRequest, error) {

	if !wsc.connected {

		//attempt reconnect?

		return nil, errors.New("WebSocket Client not Connected")

	}

	if !wsc.ready {

		return nil, errors.New("WebSocket Connection Not Ready Yet. Awaiting Response from Server.")

	}

	reqID, err := vutils.UUID.MakeUUIDStringNoDashes()

	if err != nil {

		return nil, err

	}

	reqBody := vannws_utils.WebSocketRequestBody{
		MessageType: vannws_utils.BasicMessage,
		ID:          reqID,
		Payload:     payload,
	}

	println(payload, reqBody)
	//create the item on the request stack...

	wsc.reqStack[reqID] = vannws_utils.NewBasicWSRequest(reqID, &reqBody)

	go SendJSONRequest(reqID, wsc.conn, reqBody, wsc.reqStack[reqID])

	return wsc.reqStack[reqID], nil

}

//send JSON request will send a message to the server for processing - there are several types of JSON message this is the lowest level
func SendJSONRequest(requestID string, conn *websocketjs.WebSocket, payload interface{}, req *vannws_utils.WSRequest) {

	encMsg, err := json.Marshal(payload)

	if err == nil {

		if sendErr := conn.Send(string(encMsg)); err != nil {

			req.Errors = append(req.Errors, sendErr)

			req.Done <- false

			req.Response <- vannws_utils.NewWSRequestLocalErrorResponse(requestID, conn.GetSeshKey())

			conn.Close()

		}

	} else {

		req.Errors = append(req.Errors, errors.New("Error marshalling payload to JSON"))
		req.Errors = append(req.Errors, err)

		req.Done <- false

		req.Response <- vannws_utils.NewWSRequestLocalErrorResponse(requestID, conn.GetSeshKey())

	}

}
