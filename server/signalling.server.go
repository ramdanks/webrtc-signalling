package server

import (
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type room struct {
	host       *websocket.Conn
	guess      *websocket.Conn
	checkpoint time.Time
}

type SignalKey = string

const (
	SignalConnAsHost    SignalKey = "host"
	SignalConnAsGuess   SignalKey = "guess"
	SignalExchangeBegin SignalKey = "begin"
)

const (
	DeadlineRoomNoGuess   time.Duration = 1 * time.Minute
	DeadlineRoomWithGuess time.Duration = 10 * time.Second
)

type SignallingServer struct {
	rooms              map[string]*room
	roomsMutex         sync.Mutex
	timeKeeperLaunched bool

	WebSocketServer
}

func NewSignallingServer() *SignallingServer {
	return &SignallingServer{
		rooms:              make(map[string]*room),
		roomsMutex:         sync.Mutex{},
		timeKeeperLaunched: false,
	}
}

func (this *SignallingServer) EndpointPattern() string {
	return "/signalling/{id}"
}

func (this *SignallingServer) ServeWebSocket(wsi *WebSocketInfo) {
	s := &state{wsi: wsi, server: this}
	ServeWebSocketAsEvent(wsi, s)
}

func (this *SignallingServer) LaunchTimeKeeper() {

	// time keeper should only be launched once from the main thread
	if this.timeKeeperLaunched {
		return
	}

	// run time keeper function for the rest of the program to do the cleaning services
	this.timeKeeperLaunched = true
	go func() {

		// the infinite loop
		for {

			// for every iteration of cleaning, lock the rooms with mutex
			this.roomsMutex.Lock()

			for id, r := range this.rooms {

				// flags for the timekeeper to close the room
				close := false

				// oops, this room should be deleted, why it still exists?
				if r.host == nil && r.guess == nil {
					log.Printf("waste room is detected: %v", id)
					continue
				}

				// timeout logic validation
				if r.guess == nil {
					elapsed := time.Now().Sub(r.checkpoint)
					close = elapsed >= DeadlineRoomNoGuess
				} else {
					elapsed := time.Now().Sub(r.checkpoint)
					close = elapsed >= DeadlineRoomWithGuess
				}

				// close the room, disconnect every connection associated with the room
				if close {
					deadline := time.Now().Add(2 * time.Second)
					payload := websocket.FormatCloseMessage(CloseTimeout, "")
					if r.host != nil {
						go r.host.WriteControl(websocket.CloseMessage, payload, deadline)
					}
					if r.guess != nil {
						go r.guess.WriteControl(websocket.CloseMessage, payload, deadline)
					}
				}
			}

			// release the lock before going sleep
			this.roomsMutex.Unlock()

			// sleep for 1 second
			time.Sleep(time.Second)
		}
	}()
}

type state struct {
	id     string
	wsi    *WebSocketInfo
	server *SignallingServer

	WebSocketServerEventListener
}

func (this *state) OnInit() CloseRequestCode {
	this.id = this.wsi.pathparams["id"]
	if this.id == "" {
		return CloseUnsupportedData
	}

	time.Now()

	this.server.roomsMutex.Lock()
	defer this.server.roomsMutex.Unlock()

	r, r_exists := this.server.rooms[this.id]

	if !r_exists {
		this.server.rooms[this.id] = &room{host: this.wsi.conn, checkpoint: time.Now()}
		this.wsi.conn.WriteMessage(websocket.BinaryMessage, []byte(SignalConnAsHost))
	} else if r.guess == nil {
		r.guess = this.wsi.conn
		r.checkpoint = time.Now()
		r.guess.WriteMessage(websocket.BinaryMessage, []byte(SignalConnAsGuess))
		r.host.WriteMessage(websocket.BinaryMessage, []byte(SignalExchangeBegin))
	} else {
		return CloseTryAgainLater
	}

	return CloseNone
}

func (this *state) OnMessage(msgType int, msg []byte) CloseRequestCode {

	this.server.roomsMutex.Lock()
	defer this.server.roomsMutex.Unlock()

	room := this.server.rooms[this.id]

	if this.wsi.conn == room.host {
		// Host send a message prematurely. The host should only start the exchange if and only if the server informs via [SignalExchangeBegin] which also indicates that the guess is present. This action is prohibited, close the socket to this host due to violation.
		if room.guess == nil {
			return ClosePolicyViolation
		}
		room.guess.WriteMessage(msgType, msg)
	} else {
		room.host.WriteMessage(msgType, msg)
	}

	return CloseNone
}

func (this *state) OnClose() {

	// this func is all about mutating rooms information
	this.server.roomsMutex.Lock()
	defer this.server.roomsMutex.Unlock()

	room, exists := this.server.rooms[this.id]

	// the host might already delete the room
	if exists {
		// the host is responsible to delete the room while the guest should release it's spot for others to be able to join
		if this.wsi.conn == room.host {
			delete(this.server.rooms, this.id)
		} else {
			room.guess = nil
		}
	}
}
