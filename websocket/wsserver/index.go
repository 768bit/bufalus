package wsserver

import (
	"encoding/json"
	"errors"
	"github.com/768bit/vutils"
	"github.com/gobuffalo/buffalo"
	"github.com/google/uuid"
	"gitlab.768bit.com/vann/libvann/vannws_utils"
	"gitlab.768bit.com/vann/websocket"
	"log"
	"net/http"
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
	key           uuid.UUID
	keyString     string
	userUUID      string
	subscriptions map[string]string
	expiry        time.Time
	byteSessions  map[string]string
	sessionID     string
	jwtTicketID   string
}

type StubHandler func(userUUID string, sessionID string, jwtTicketID string, method string, reqPath string, payload interface{}, options interface{}) (interface{}, error)

type BasicHandler func(conn *websocket.Conn, mt WebsocketMessageType, reqID string, payload interface{}) error

type WebSocketServer struct {
	sessions          map[string]*WSSession
	authProvider      IAuthProvider
	userStoreProvider IUserStoreProvider
	stubHandler       StubHandler
	basicHandler      BasicHandler
	isBasic           bool
}

func NewWebSocketServer(authProvider IAuthProvider, userStoreProvider IUserStoreProvider, stubHandler StubHandler) *WebSocketServer {

	return &WebSocketServer{
		sessions:          map[string]*WSSession{},
		authProvider:      authProvider,
		userStoreProvider: userStoreProvider,
		stubHandler:       stubHandler,
		isBasic:           false,
	}

}

func NewBasicWebSocketServer(handler BasicHandler) *WebSocketServer {

	return &WebSocketServer{
		sessions:     map[string]*WSSession{},
		basicHandler: handler,
		isBasic:      true,
	}

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
				log.Println("read:", err)
				break
			}
			log.Printf("recv: %s -> %s", mt, message)
			err = ws.processReceiveMessage(c, mt, message)
			//err = c.WriteMessage(mt, message)
			if err != nil {
				log.Println("write:", err)
				break
			}
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
				log.Println("read:", err)
				break
			}
			log.Printf("recv: %s -> %s", mt, message)
			err = ws.processReceiveMessage(c, mt, message)
			//err = c.WriteMessage(mt, message)
			if err != nil {
				log.Println("write:", err)
				break
			}
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
		return vannws_utils.HandleByteStream(conn, data)
		//case websocket.CloseMessage:
		//handle close message - the client has requested a close go ahead and action that...

	}

	return errors.New("Not implemented")

}

func (ws *WebSocketServer) processJSONRequest(conn *websocket.Conn, data []byte) error {

	//process the inbound JSON message - we need to peek try and coerce it to a type, figure out whats going on with it...
	// there is only one "request" type - requests come from clients to the server so we coerce to it and ccheck the message type

	//marshall the request into the Request Fromat

	var requestBody vannws_utils.WebSocketRequestBody

	if err := json.Unmarshal(data, &requestBody); err != nil {

		//error marshalling data from JSON...

		println("Error unmarshalling", err)

		return err

	} else {

		//now we have the payload lets look at it..

		switch requestBody.MessageType {
		case vannws_utils.RPCSessionStartMessage:
			//we need to begin a session
			if ws.isBasic {
				return errors.New("Cannot start a session against a simple WebSocket server")
			}
			return ws.newWSSession(conn, requestBody.ID, requestBody.Payload["userUUID"].(string), requestBody.Payload["jwtTicketID"].(string))
		case vannws_utils.HTTPMessage:
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
					return vannws_utils.SendJSONMessage(conn, vannws_utils.NewWebSocketHttpResponseBody(vannws_utils.RPCStatusOK,
						requestBody.SeshKey, requestBody.ID, requestBody.Method, requestBody.Path, responsePayload))
				}

			}
		case vannws_utils.BasicMessage:
			if ws.isBasic {
				return ws.basicHandler(conn, TextMessage, requestBody.ID, requestBody.Payload)
			}
			return errors.New("Cannot process basic messages as this websocket server is not in basic mode")
		}

	}

	return errors.New("Not implemented")

}

func NewWebSocketServerHelloResponseMessage() vannws_utils.WebSocketResponseBody {
	return vannws_utils.WebSocketResponseBody{
		MessageType: vannws_utils.ServerHelloMessage,
		StatusCode:  vannws_utils.RPCStatusOK,
	}
}

func sendConnectionBeginMessage(conn *websocket.Conn) error {

	//send a json message saying HELO only the server does this to alert the client it is ready to receive data

	//no need for a req object here.. we wont wait for a response - we will just start processing any messages from the client immediately

	return vannws_utils.SendJSONMessage(conn, NewWebSocketServerHelloResponseMessage())

}

//now for handling json payloads...

func (ws *WebSocketServer) newWSSession(conn *websocket.Conn, requestID string, userUUID string, jwtTicketID string) error {

	//make the new WSSession on *this* server and maintain it until cleaned up...

	//need to claim the ticket first...

	if claimed, seshID := ws.userStoreProvider.UseJWTTicketForUser(userUUID, jwtTicketID); claimed == true && seshID != "" {

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
				expiry:       time.Now().Add(time.Duration(15*60) * time.Second),
				keyString:    seshKeyStr,
				key:          *seshKey,
				userUUID:     userUUID,
				byteSessions: map[string]string{},
				sessionID:    seshID,
				jwtTicketID:  jwtTicketID,
			}

			return vannws_utils.SendJSONMessage(conn, vannws_utils.NewWebSocketSessionStartResponseBody(requestID, seshKeyStr))

		}

	} else {

		return errors.New("Unable to claim JWT ticket")

	}

}

func (ws *WebSocketServer) closeWSSession(conn *websocket.Conn) {

	//clean up and close the connection...

	//signal client that their sessions are terminated...

}

func (ws *WebSocketServer) newWSStreamSession() {

	//the client intends to start a stream session with the server

}

func (ws *WebSocketServer) closeWSStreamSession(conn *websocket.Conn) {

	//clean up and close the connection...

	//signal client that their sessions are terminated...

}
