package client

import (
	"encoding/json"
	"errors"
	"github.com/768bit/go_wsutils"
	"github.com/768bit/vutils"
	"github.com/768bit/websocket"
	"log"
	"net/url"
)

type WebSocketClient struct {
	conn        *websocket.Conn
	host        string
	scheme      string
	path        string
	reqStack    map[string]*go_wsutils.WSRequest
	reqQueue    []*go_wsutils.WSRequest
	ReadyChan   chan bool
	doneChan    chan bool
	errChan     chan error
	ready       bool
	connected   bool
	authorised  bool
	session     *go_wsutils.WSClientSession
	jwtTicketID string
}

func NewWebSocketClient(scheme string, host string, path string) *WebSocketClient {

	wsc := &WebSocketClient{
		scheme:     scheme,
		host:       host,
		path:       path,
		reqStack:   map[string]*go_wsutils.WSRequest{},
		ReadyChan:  make(chan bool),
		ready:      false,
		connected:  false,
		reqQueue:   []*go_wsutils.WSRequest{},
		authorised: false,
	}

	//go wsc.connectWS()

	return wsc

}

func (wsc *WebSocketClient) ConnectWS(jwtTicketID string, doneChan chan bool, errChan chan error) {

	println("Starting Websocket Connection")
	wsc.jwtTicketID = jwtTicketID
	u := url.URL{Scheme: wsc.scheme, Host: wsc.host, Path: "/_ws"}
	log.Printf("connecting to %s", u.String())
	wsc.doneChan = doneChan
	wsc.errChan = errChan

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		errChan <- err
		close(doneChan)
		close(errChan)
		return
	}
	wsc.conn = c
	wsc.connected = true
	go func() {
		defer close(doneChan)
		defer close(errChan)
		for {
			mt, message, err := c.ReadMessage()
			if err != nil {
				errChan <- err
				return
			}
			switch mt {
			case websocket.BinaryMessage:
				go_wsutils.HandleByteStream(wsc.conn, message)
			case websocket.TextMessage:
				//handle text message (json)
			case websocket.CloseMessage:
				//handle the close - its unusual for the client to receive this from the server but if it does unwind any pending requests immediately
				return
			}
		}
	}()

}

func (wsc *WebSocketClient) handleReceiveJSON(conn *websocket.Conn, data []byte) {

	//need to lookup the request in the stack signal done then pass the response... if the response is an error that can be handled too...

	//this could be any type of message and is a low level handler that will coerce the available types and make it

	//need to figure out what the basic parameters are (message type, request id etc..)

	//coerce to the go_wsutils.WebSocketResponseBody type

	var responseBody go_wsutils.WebSocketResponseBody

	if err := json.Unmarshal(data, &responseBody); err != nil {

		//error marshalling data to JSON...

	} else {

		//content is fine figure out what it is...

		switch responseBody.MessageType {

		case go_wsutils.ServerHelloMessage:
			wsc.connected = true
			wsc.ready = true
			//immediately negotiate a session so we can ready the client

			wsc.ReadyChan <- wsc.newWSSession() //send the ready signal to the channel - we can start processing requests...

		default:

		}

	}

}

func (wsc *WebSocketClient) processJSONResponse(conn *websocket.Conn, response *go_wsutils.WebSocketResponseBody) {

	//need to verify the session etc...

}

func (wsc *WebSocketClient) SendRpcRequest(modURI string, cmd string, path string, payload map[string]interface{},
	options map[string]interface{}, headers map[string]string) (*go_wsutils.WSRequest, error) {

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

	reqBody := go_wsutils.WebSocketRequestBody{
		MessageType: go_wsutils.RPCMessage,
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

	wsc.reqStack[reqID] = go_wsutils.NewWSRequest(reqID, wsc.conn.GetSeshKey(), &reqBody)

	go_wsutils.SendJSONRequest(reqID, wsc.conn, reqBody, wsc.reqStack[reqID])

	return wsc.reqStack[reqID], nil

}

func (wsc *WebSocketClient) SendHttpRequest(method string, path string, payload map[string]interface{},
	options map[string]interface{}, headers map[string]string) (*go_wsutils.WSRequest, error) {

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

	reqBody := go_wsutils.WebSocketRequestBody{
		MessageType: go_wsutils.HTTPMessage,
		Method:      method,
		ID:          reqID,
		SeshKey:     wsc.conn.GetSeshKey(),
		Path:        path,
		Options:     options,
		Payload:     payload,
		Headers:     headers,
	}

	//create the item on the request stack...

	wsc.reqStack[reqID] = go_wsutils.NewWSHttpRequest(reqID, wsc.conn.GetSeshKey(), &reqBody)

	go_wsutils.SendJSONRequest(reqID, wsc.conn, reqBody, wsc.reqStack[reqID])

	return wsc.reqStack[reqID], nil

}

func (wsc *WebSocketClient) closeWS() {

	//close the session on the server then terminate the connection...
	err := wsc.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	if err != nil {
		log.Println("write close:", err)
		return
	}

}

//now for handling json payloads...

func (wsc *WebSocketClient) newWSSession() bool {

	//make the new WSSession on *this* server and maintain it until cleaned up...

	reqID, err := vutils.UUID.MakeUUIDStringNoDashes()

	if err != nil {

		return false

	}

	reqBody := go_wsutils.NewWebSocketSessionStartRequestBody(reqID, wsc.jwtTicketID, "")

	req := go_wsutils.NewWSRequestWithTimeout(reqID, wsc.conn.GetSeshKey(), reqBody, 15)

	wsc.reqStack[reqID] = req

	go_wsutils.SendJSONRequest(reqID, wsc.conn, reqBody, wsc.reqStack[reqID])

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

			} else if resp.MessageType == go_wsutils.RPCSessionStartMessage && resp.StatusCode == go_wsutils.RPCStatusOK {
				//handle the response as required...

			} else if resp.MessageType == go_wsutils.RPCSessionStartErrorMessage {
				//handle the response as required...

			} else {

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

func (ws *WebSocketClient) closeWSStreamSession(conn *websocket.Conn) {

	//the server will have responded in kind to the close request or we are cleaning up the sessions client side....

}
