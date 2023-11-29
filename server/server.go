package server

import (
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

// MARK: Common Types

type WebSocketInfo struct {
	conn       *websocket.Conn
	request    *http.Request
	pathparams map[string]string
}

// MARK: WebSocketServer

type WebSocketServerInterface interface {
	EndpointPattern() string
	ServeWebSocket(*WebSocketInfo)
}

type WebSocketServer struct {
	WebSocketServerInterface
}

func (server *WebSocketServer) EndpointPattern() string {
	return "/"
}

func (server *WebSocketServer) ServeWebSocket(wsi *WebSocketInfo) {
	log.Printf("Unimplemented ServeWebSocket: %v", wsi.request.URL)
}

// MARK: WebSocket Event

type CloseRequestCode = int

// Close codes defined in RFC 6455, section 11.7.
// With the exception of CloseNone is behavioral flags for WebSocketEventBasedServer
const (
	CloseNone                    CloseRequestCode = 0
	CloseNormalClosure           CloseRequestCode = 1000
	CloseGoingAway               CloseRequestCode = 1001
	CloseProtocolError           CloseRequestCode = 1002
	CloseUnsupportedData         CloseRequestCode = 1003
	CloseNoStatusReceived        CloseRequestCode = 1005
	CloseAbnormalClosure         CloseRequestCode = 1006
	CloseInvalidFramePayloadData CloseRequestCode = 1007
	ClosePolicyViolation         CloseRequestCode = 1008
	CloseMessageTooBig           CloseRequestCode = 1009
	CloseMandatoryExtension      CloseRequestCode = 1010
	CloseInternalServerErr       CloseRequestCode = 1011
	CloseServiceRestart          CloseRequestCode = 1012
	CloseTryAgainLater           CloseRequestCode = 1013
	CloseTLSHandshake            CloseRequestCode = 1015
	CloseTimeout                 CloseRequestCode = 4000
)

type WebSocketServerEventListener interface {
	OnInit() CloseRequestCode
	OnMessage(int, []byte) CloseRequestCode
	OnClose()
}

// Mark: Server Implementation

func ServeWebSocketFromHTTP(
	w http.ResponseWriter,
	r *http.Request,
	server WebSocketServerInterface,
) {
	wsi := upgradeHTTPConnectionToWebSocket(w, r)
	server.ServeWebSocket(wsi)
}

func ServeWebSocketAsEvent(
	wsi *WebSocketInfo,
	listener WebSocketServerEventListener,
) {
	// when the closeCode is set to other than CloseNone, the server wants to disconnect / close the socket with the client
	var closeCode CloseRequestCode = CloseNone

	// tells the handler that websocket connection is established
	closeCode = listener.OnInit()
	if closeCode != CloseNone {
		goto End
	}

	for {

		// heads up! connection close may be notified in err variable
		msgType, msg, err := wsi.conn.ReadMessage()

		// handle error as well as connection close event.
		if err != nil {

			// check whether the client is closing the socket
			e, isClientCloseSocket := err.(*websocket.CloseError)
			if !isClientCloseSocket {
				closeCode = CloseInternalServerErr
			}

			// log upon anomaly close
			if e != nil && e.Code != websocket.CloseNormalClosure {
				log.Println(err)
			}

			// finish the main loop
			break
		}

		// tells the listener to receive the message
		closeCode = listener.OnMessage(msgType, msg)
		if closeCode != CloseNone {
			goto End
		}
	}

End:
	// handle socket termination to follow WebSocket protocol correctly
	if closeCode != CloseNone {
		// initiate socket close to client with deadline
		deadline := time.Now().Add(2 * time.Second)
		payload := websocket.FormatCloseMessage(closeCode, "")
		err := wsi.conn.WriteControl(websocket.CloseMessage, payload, deadline)

		// if fail, then force close. no need to wait for closing ack response
		if err != nil && err != websocket.ErrCloseSent {
			log.Printf("GroupChatServer error while closing socket : %v", err)
		}
	}

	// tells the listener before closing the connection
	listener.OnClose()
	wsi.conn.Close()
}

// MARK: Utility Private Functions

// Used when the http receive a request, and we need to upgrade the connection to a WebSocket
func upgradeHTTPConnectionToWebSocket(
	w http.ResponseWriter,
	r *http.Request,
) *WebSocketInfo {
	u := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	c, err := u.Upgrade(w, r, nil)
	if err != nil {
		log.Fatalf("failed to upgrade to WebSocket connection: %v", err)
	}
	return &WebSocketInfo{conn: c, request: r, pathparams: mux.Vars(r)}
}
