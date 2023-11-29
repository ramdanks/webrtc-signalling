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

func (server *SignallingServer) EndpointPattern() string {
	return "/signalling/{id}"
}

func (server *SignallingServer) ServeWebSocket(wsi *WebSocketInfo) {
	s := &state{wsi: wsi, server: server}
	ServeWebSocketAsEvent(wsi, s)
}

func (server *SignallingServer) LaunchTimeKeeper() {

	// time keeper should only be launched once from the main thread
	if server.timeKeeperLaunched {
		return
	}

	// run time keeper function for the rest of the program to do the cleaning services
	server.timeKeeperLaunched = true
	go func() {

		// the infinite loop
		for {

			// for every iteration of cleaning, lock the rooms with mutex
			server.roomsMutex.Lock()

			for id, r := range server.rooms {

				// flags for the timekeeper to close the room
				close := false

				// oops, this room should be deleted, why it still exists?
				if r.host == nil && r.guess == nil {
					log.Printf("waste room is detected: %v", id)
					continue
				}

				// timeout logic validation
				if r.guess == nil {
					elapsed := time.Since(r.checkpoint)
					close = elapsed >= DeadlineRoomNoGuess
				} else {
					elapsed := time.Since(r.checkpoint)
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
			server.roomsMutex.Unlock()

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

func (s *state) OnInit() CloseRequestCode {
	s.id = s.wsi.pathparams["id"]
	if s.id == "" {
		return CloseUnsupportedData
	}

	s.server.roomsMutex.Lock()
	defer s.server.roomsMutex.Unlock()

	r, r_exists := s.server.rooms[s.id]

	if !r_exists {
		s.server.rooms[s.id] = &room{host: s.wsi.conn, checkpoint: time.Now()}
		s.wsi.conn.WriteMessage(websocket.BinaryMessage, []byte(SignalConnAsHost))
	} else if r.guess == nil {
		r.guess = s.wsi.conn
		r.checkpoint = time.Now()
		r.guess.WriteMessage(websocket.BinaryMessage, []byte(SignalConnAsGuess))
		r.host.WriteMessage(websocket.BinaryMessage, []byte(SignalExchangeBegin))
	} else {
		return CloseTryAgainLater
	}

	return CloseNone
}

func (s *state) OnMessage(msgType int, msg []byte) CloseRequestCode {

	s.server.roomsMutex.Lock()
	defer s.server.roomsMutex.Unlock()

	room := s.server.rooms[s.id]

	if s.wsi.conn == room.host {
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

func (s *state) OnClose() {

	// this func is all about mutating rooms information
	s.server.roomsMutex.Lock()
	defer s.server.roomsMutex.Unlock()

	room, exists := s.server.rooms[s.id]

	// the host might already delete the room
	if exists {
		// the host is responsible to delete the room while the guest should release it's spot for others to be able to join
		if s.wsi.conn == room.host {
			delete(s.server.rooms, s.id)
		} else {
			room.guess = nil
		}
	}
}
