package wsserver

import (
	"encoding/json"
	"errors"
	"github.com/768bit/go_wsutils"
	"github.com/768bit/vutils"
	"github.com/768bit/websocket"
	"github.com/cskr/pubsub"
	"github.com/gobuffalo/buffalo"
	"github.com/google/uuid"
	"log"
	"net/http"
	"sync"
	"time"
)

var upgrader = websocket.Upgrader{}

type WebsocketMessageType int

const (
	TextMessage   WebsocketMessageType = websocket.TextMessage
	BinaryMessage WebsocketMessageType = websocket.BinaryMessage
)

func internalError(conn *websocket.Conn, msg string, err error) {
	log.Println(msg, err)
	conn.WriteMessage(websocket.TextMessage, []byte("Internal server error."))
}

type WSSession struct {
	ws             *WebSocketServer
	conn           *websocket.Conn
	key            uuid.UUID
	keyString      string
	userUUID       string
	subscriptions  map[string]string
	expiry         time.Time
	byteSessions   map[string]string
	sessionID      string
	jwtTicketID    string
	psMapMutex     *sync.Mutex
	subMap         map[string]chan interface{}
	subExitMap     map[string]chan bool
	agg            chan *WSPublishMesssage
	psExitChan     chan bool
	hasSub         bool
	subProcStarted bool
}

type WSPublishMesssage struct {
	Topic string
	Data  interface{}
}

func (wss *WSSession) runPubSubProcessor() {
	if wss.subProcStarted {
		return
	}
	wss.subProcStarted = true
	wss.agg = make(chan *WSPublishMesssage)
	go func() {
		for {
			select {
			case <-wss.psExitChan:
				//exit signal received - terminate all channels...
				wss.psMapMutex.Lock()
				for topic, _ := range wss.subMap {
					wss.unSubscribe(topic)
				}
				wss.hasSub = false
				wss.subProcStarted = false
				close(wss.agg)
				wss.psMapMutex.Unlock()
				return
			case msg := <-wss.agg:
				wss.psMapMutex.Lock()
				if err := go_wsutils.SendJSONMessage(wss.conn, go_wsutils.NewWebSocketPublishBody(go_wsutils.RPCStatusOK,
					wss.keyString, msg.Topic, msg.Data)); err != nil {
					log.Println("Error Sending Publish Messsage:", err)
				}
				wss.psMapMutex.Unlock()
			}
		}
	}()
}

func (wss *WSSession) Subscribe(topic string) {
	wss.psMapMutex.Lock()
	defer wss.psMapMutex.Unlock()
	if v, ok := wss.subMap[topic]; !ok || v == nil {
		log.Println("Subscribing", wss.userUUID, "to", topic)
		wss.subMap[topic] = wss.ws.pubsub.Sub(topic)
		wss.subExitMap[topic] = make(chan bool)
		if !wss.hasSub {
			wss.hasSub = true
		}
		if !wss.subProcStarted {
			wss.runPubSubProcessor()
		}
		go func(topic string, recvChan chan interface{}, aggCh chan *WSPublishMesssage, exitChan chan bool) {
			for {
				select {
				case <-exitChan:
					close(exitChan)
					delete(wss.subExitMap, topic)
					return
				case msg := <-recvChan:
					am := &WSPublishMesssage{
						Topic: topic,
						Data:  msg,
					}
					aggCh <- am
				}
			}
		}(topic, wss.subMap[topic], wss.agg, wss.subExitMap[topic])
	}
}

func (wss *WSSession) UnSubscribe(topic string) {
	wss.psMapMutex.Lock()
	defer wss.psMapMutex.Unlock()
	wss.unSubscribe(topic)
	if len(wss.subMap) == 0 {
		if wss.subProcStarted {
			wss.psExitChan <- true
		}
	}
}

func (wss *WSSession) unSubscribe(topic string) {
	if v, ok := wss.subMap[topic]; !ok || v == nil {
		return
	} else {
		log.Println("Unsubscribing", wss.userUUID, "from", topic)
		wss.ws.pubsub.Unsub(wss.subMap[topic])
		delete(wss.subMap, topic)
		wss.subExitMap[topic] <- true
		delete(wss.subExitMap, topic)
	}
}

type StubHandler func(userUUID string, sessionID string, jwtTicketID string, method string, reqPath string, payload interface{}, options interface{}) (interface{}, error)

type BasicHandler func(conn *websocket.Conn, mt WebsocketMessageType, reqID string, payload interface{}) error

type RPCHandler func(req *go_wsutils.WebSocketRequestBody, cmd string, payload interface{}, options map[string]interface{}) (interface{}, error)

type WebSocketServer struct {
	sessions        map[string]*WSSession
	authProvider    IAuthProvider
	sessionProvider ISessionProvider
	stubHandler     StubHandler
	basicHandler    BasicHandler
	rpcHandler      RPCHandler
	isBasic         bool
	pubsub          *pubsub.PubSub
}

func NewWebSocketServer(authProvider IAuthProvider, sessionProvider ISessionProvider, stubHandler StubHandler) *WebSocketServer {

	return &WebSocketServer{
		sessions:        map[string]*WSSession{},
		authProvider:    authProvider,
		sessionProvider: sessionProvider,
		stubHandler:     stubHandler,
		isBasic:         false,
		pubsub:          pubsub.New(10),
	}

}

func NewBasicWebSocketServer(handler BasicHandler) *WebSocketServer {

	return &WebSocketServer{
		sessions:     map[string]*WSSession{},
		basicHandler: handler,
		isBasic:      true,
		pubsub:       pubsub.New(10),
	}

}

func (ws *WebSocketServer) RegisterRPCHandler(handler RPCHandler) {

	ws.rpcHandler = handler

}

func (ws *WebSocketServer) Publish(topic string, data interface{}) {

	ws.pubsub.Pub(data, topic)

}

func (ws *WebSocketServer) ServeWS() buffalo.Handler {
	return buffalo.WrapHandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Print("Connection WebSocket Upgrade Error: ", err)
			return
		}
		log.Print("WebSocket Connection Established")
		//SEND WELCOME MESSAGE - USER WILL BEGIN LOGIN PROCESS
		err = sendConnectionBeginMessage(c)
		if err != nil {
			log.Println("Send Server HELLO Error: ", err)
			c.Close()
			return
		}
		defer c.Close()
		for {
			mt, message, err := c.ReadMessage()
			if err != nil {
				log.Println("read_error:", err)
				if !ws.isBasic {
					if seshKey := c.GetSeshKey(); seshKey != "" {
						if wssesh := ws.sessions[seshKey]; wssesh == nil {
							ws.closeWSSession(wssesh)
						}
					}
				}
				break
			}
			log.Printf("recv: %s -> %s", mt, message)
			go func(imt int, imsg []byte) {
				err = ws.processReceiveMessage(c, imt, imsg)
				//err = c.WriteMessage(mt, message)
				if err != nil {
					log.Println("recv_proc_error:", err)
				}
			}(mt, message)
		}
	})
}

func (ws *WebSocketServer) ServeBasicWSHandler() buffalo.Handler {
	return buffalo.WrapHandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Print("Connection WebSocket Upgrade Error: ", err)
			return
		}
		log.Print("WebSocket Connection Established")
		err = sendConnectionBeginMessage(c)
		if err != nil {
			log.Println("Send Server HELLO Error: ", err)
			c.Close()
			return
		}
		defer c.Close()
		for {
			mt, message, err := c.ReadMessage()
			if err != nil {
				log.Println("read_error:", err)
				break
			}
			log.Printf("recv: %s -> %s", mt, message)
			go func(imt int, imsg []byte) {
				err = ws.processReceiveMessage(c, imt, imsg)
				//err = c.WriteMessage(mt, message)
				if err != nil {
					log.Println("recv_proc_error:", err)
				}
			}(mt, message)
		}
	})
}

func (ws *WebSocketServer) ServeBasicWS(connHandler func(conn *websocket.Conn)) buffalo.Handler {
	return buffalo.WrapHandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Print("Connection WebSocket Upgrade Error: ", err)
			return
		}
		log.Print("WebSocket Connection Established")
		connHandler(c)
	})
}

func (ws *WebSocketServer) processReceiveMessage(conn *websocket.Conn, mt int, data []byte) error {

	switch mt {

	case websocket.TextMessage:
		//process text message (json) - session start, http reqs, rpc messages etc..
		//if ws.isBasic {
		//  return ws.basicHandler(mt, data)
		//}
		return ws.processJSONRequest(conn, data)
	case websocket.BinaryMessage:
		//process a stream of binary data...
		if ws.isBasic {
			return ws.basicHandler(conn, BinaryMessage, "", data)
		}
		return go_wsutils.HandleByteStream(conn, data)
		//case websocket.CloseMessage:
		//handle close message - the client has requested a close go ahead and action that...

	}

	return errors.New("Not implemented")

}

func (ws *WebSocketServer) processJSONRequest(conn *websocket.Conn, data []byte) error {

	//process the inbound JSON message - we need to peek try and coerce it to a type, figure out whats going on with it...
	// there is only one "request" type - requests come from clients to the server so we coerce to it and ccheck the message type

	//marshall the request into the Request Fromat

	var requestBody go_wsutils.WebSocketRequestBody

	if err := json.Unmarshal(data, &requestBody); err != nil {

		//error marshalling data from JSON...

		println("Error unmarshalling", err)

		return err

	} else {

		//now we have the payload lets look at it..

		requestBody.SetConn(conn).CreateContext()

		switch requestBody.MessageType {
		case go_wsutils.RPCSessionStartMessage:
			//we need to begin a session
			if ws.isBasic {
				return errors.New("Cannot start a session against a simple WebSocket server")
			}
			if err := ws.newWSSession(conn, requestBody.ID, requestBody.Payload["userUUID"].(string), requestBody.Payload["jwtTicketID"].(string)); err != nil {
				log.Println("Error Starting Session for", requestBody.Payload["userUUID"].(string), err)
				return go_wsutils.SendJSONMessage(conn, go_wsutils.NewWebSocketSessionStartErrorResponseBody(requestBody.ID, go_wsutils.RPCStatusUnauthorised, err))
			} else {
				return nil
			}
		case go_wsutils.SubscribeMessage:
			if ws.isBasic {
				return errors.New("Cannot subscribe with a simple WebSocket server")
			}
			if wssesh := ws.sessions[requestBody.SeshKey]; wssesh == nil {

				//try lookup the

				err := errors.New("Unable to get session with that key")
				return go_wsutils.SendJSONMessage(conn, go_wsutils.NewWebSocketSubscribeErrorResponseBody(go_wsutils.RPCStatusUnauthorised,
					requestBody.SeshKey, requestBody.ID, requestBody.Topic, err.Error()))

			} else {
				wssesh.Subscribe(requestBody.Topic)
				return go_wsutils.SendJSONMessage(conn, go_wsutils.NewWebSocketSubscribeResponseBody(go_wsutils.RPCStatusOK,
					requestBody.SeshKey, requestBody.ID, requestBody.Topic))
			}
		case go_wsutils.UnSubscribeMessage:
			if ws.isBasic {
				return errors.New("Cannot unsubscribe with a simple WebSocket server")
			}
			if wssesh := ws.sessions[requestBody.SeshKey]; wssesh == nil {

				//try lookup the

				err := errors.New("Unable to get session with that key")
				return go_wsutils.SendJSONMessage(conn, go_wsutils.NewWebSocketUnSubscribeErrorResponseBody(go_wsutils.RPCStatusUnauthorised,
					requestBody.SeshKey, requestBody.ID, requestBody.Topic, err.Error()))

			} else {
				wssesh.UnSubscribe(requestBody.Topic)
				return go_wsutils.SendJSONMessage(conn, go_wsutils.NewWebSocketUnSubscribeResponseBody(go_wsutils.RPCStatusOK,
					requestBody.SeshKey, requestBody.ID, requestBody.Topic))
			}
		case go_wsutils.RPCMessage:
			if ws.isBasic {
				return errors.New("Cannot start a session against a simple WebSocket server")
			}
			if wssesh := ws.sessions[requestBody.SeshKey]; wssesh == nil {

				//try lookup the

				err := errors.New("Unable to get session with that key")
				return go_wsutils.SendJSONMessage(conn, go_wsutils.NewWebSocketRPCErrorResponseBody(go_wsutils.RPCStatusUnauthorised,
					requestBody.SeshKey, requestBody.ID, requestBody.Cmd, nil, err.Error()))

			} else {

				log.Print("Attempting RPC call via RPCHandler")

				requestBody.SetSessionDetails(wssesh.sessionID, wssesh.userUUID, wssesh.jwtTicketID)

				responsePayload, err := ws.rpcHandler(&requestBody, requestBody.Cmd, requestBody.Payload, requestBody.Options)
				if err != nil {
					return go_wsutils.SendJSONMessage(conn, go_wsutils.NewWebSocketRPCErrorResponseBody(go_wsutils.RPCStatusError,
						requestBody.SeshKey, requestBody.ID, requestBody.Cmd, responsePayload, err.Error()))
				} else {
					//package and send response...
					return go_wsutils.SendJSONMessage(conn, go_wsutils.NewWebSocketRPCResponseBody(go_wsutils.RPCStatusOK,
						requestBody.SeshKey, requestBody.ID, requestBody.Cmd, responsePayload))
				}
			}
		case go_wsutils.HTTPMessage:
			//retrieve the wssession
			if ws.isBasic {
				return errors.New("Cannot start a session against a simple WebSocket server")
			}
			if wssesh := ws.sessions[requestBody.SeshKey]; wssesh == nil {

				return errors.New("Unable to get session with that key")

			} else {

				log.Print("Attempting HTTP call via API Stub")

				responsePayload, err := ws.stubHandler(wssesh.userUUID, wssesh.sessionID, wssesh.jwtTicketID, requestBody.Method,
					requestBody.Path, requestBody.Payload, requestBody.Options)

				if err != nil {

					log.Print(err)

					return err
				} else {
					//package and send response...
					return go_wsutils.SendJSONMessage(conn, go_wsutils.NewWebSocketHttpResponseBody(go_wsutils.RPCStatusOK,
						requestBody.SeshKey, requestBody.ID, requestBody.Method, requestBody.Path, responsePayload))
				}

			}
		case go_wsutils.BasicMessage:
			if ws.isBasic {
				return ws.basicHandler(conn, TextMessage, requestBody.ID, requestBody.Payload)
			}
			return errors.New("Cannot process basic messages as this websocket server is not in basic mode")
		}

	}

	return errors.New("Not implemented")

}

func NewWebSocketServerHelloResponseMessage() go_wsutils.WebSocketResponseBody {
	return go_wsutils.WebSocketResponseBody{
		MessageType: go_wsutils.ServerHelloMessage,
		StatusCode:  go_wsutils.RPCStatusOK,
	}
}

func sendConnectionBeginMessage(conn *websocket.Conn) error {

	//send a json message saying HELO only the server does this to alert the client it is ready to receive data

	//no need for a req object here.. we wont wait for a response - we will just start processing any messages from the client immediately

	return go_wsutils.SendJSONMessage(conn, NewWebSocketServerHelloResponseMessage())

}

//now for handling json payloads...

func (ws *WebSocketServer) newWSSession(conn *websocket.Conn, requestID string, userUUID string, jwtTicketID string) error {

	//make the new WSSession on *this* server and maintain it until cleaned up...

	//need to claim the ticket first...

	if claimed, seshID := ws.sessionProvider.UseJWTTicketForUser(userUUID, jwtTicketID); claimed == true && seshID != "" {

		//we have claimed the token initialise a session for the user...

		log.Printf("Successfully claimed JWT token for user %s", userUUID)

		//new sesh key...

		seshKey, seshKeyStr, err := vutils.UUID.MakeUUIDAndString()

		if err != nil {
			return err
		}

		if ws.sessions[seshKeyStr] != nil {
			return errors.New("Session with generated key already exists.")
		} else {

			conn.SetSeshKey(seshKeyStr)
			ws.sessions[seshKeyStr] = &WSSession{
				ws:           ws,
				conn:         conn,
				psExitChan:   make(chan bool),
				subMap:       map[string]chan interface{}{},
				subExitMap:   map[string]chan bool{},
				psMapMutex:   &sync.Mutex{},
				expiry:       time.Now().Add(time.Duration(15*60) * time.Second),
				keyString:    seshKeyStr,
				key:          *seshKey,
				userUUID:     userUUID,
				byteSessions: map[string]string{},
				sessionID:    seshID,
				jwtTicketID:  jwtTicketID,
			}

			return go_wsutils.SendJSONMessage(conn, go_wsutils.NewWebSocketSessionStartResponseBody(requestID, seshKeyStr))

		}

	} else {

		return errors.New("Unable to claim JWT ticket")

	}

}

func (ws *WebSocketServer) closeWSSession(wss *WSSession) {
	log.Println("Closing WSSession", wss.keyString, "for", wss.userUUID)
	if wss.hasSub {
		log.Println("Cleaning WSSession Subscriptions:", len(wss.subMap))
		wss.psExitChan <- true
	}
	delete(ws.sessions, wss.keyString)

}

func (ws *WebSocketServer) newWSStreamSession() {

	//the client intends to start a stream session with the server

}

func (ws *WebSocketServer) closeWSStreamSession(conn *websocket.Conn) {

	//clean up and close the connection...

	//signal client that their sessions are terminated...

}
