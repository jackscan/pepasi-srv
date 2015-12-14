package main

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type playerID string

type candidate struct {
	index uint
	name  string
	playerID
}

type errorResp struct {
	Error string
}

type playerSel struct {
	id    playerID
	selID playerID
	timestamp
	ch  chan interface{}
	add bool
}

type seeker struct {
	id   playerID
	name string
	ch   chan []candidate
}

type registry struct {
	// query      chan interface{}
	// player     map[playerID]*player
	mutex  sync.RWMutex
	seeker []*seeker
	ch     chan playerSel
}

func newRegistry() *registry {
	return &registry{
		ch: make(chan playerSel, 1),
	}
}

func handleConnection(reg *registry) func(http.ResponseWriter, *http.Request) {
	var upgrader websocket.Upgrader
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Print(err)
			return
		}
		defer c.Close()
		reg.handleClient(c)
	}
}

func (s *seeker) notify(c *candidate) (ok bool) {
	defer func() {
		ok = recover() == nil
	}()
	select {
	case s.ch <- []candidate{*c}:
	case l := <-s.ch:
		s.ch <- append(l, *c)
	default:
		ok = false
	}
	return
}

func (r *registry) addSeeker(s *seeker) (uint, map[uint]candidate) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	index := -1
	// reserve for current seekers and coming
	others := make(map[uint]candidate, len(r.seeker)*2+1)

	for i, o := range r.seeker {
		if o != nil {
			others[uint(i)] = candidate{
				index:    uint(i),
				name:     o.name,
				playerID: o.id,
			}
		} else if index < 0 {
			r.seeker[i] = s
			index = i
		}
	}
	if index < 0 {
		index = len(r.seeker)
		r.seeker = append(r.seeker, s)
	}

	nc := candidate{
		index:    uint(index),
		name:     s.name,
		playerID: s.id,
	}

	for i, o := range r.seeker {
		if o != s && !o.notify(&nc) {
			// remove abandoned seeker
			r.seeker[i] = nil
		}
	}

	return uint(index), others
}

func (r *registry) run() {
	type aspirant struct {
		playerID
		timestamp
		ch chan interface{}
	}
	players := make(map[playerID][]aspirant, 2)
loop:
	for {
		select {
		case s, ok := <-r.ch:
			if !ok {
				break loop
			}

			if s.add {
				if len(s.selID) > 0 {
					for _, c := range players[s.id] {
						if c.playerID == s.selID {
							// other player is already waiting
							a := player{c.playerID, c.ch}
							b := player{s.id, s.ch}
							startCompetition(a, b, s.timestamp-c.timestamp)
							continue loop
						}
					}

					alist, exists := players[s.selID]
					if exists {
						players[s.selID] = append(alist, aspirant{s.id, s.timestamp, s.ch})
					} else {
						// selected player is already gone
						close(s.ch)
					}
				} else {
					players[s.id] = make([]aspirant, 0, 1)
				}
			} else {
				if len(s.selID) > 0 {
					// remove player from other list
					alist := players[s.selID]
					for i := len(alist) - 1; i >= 0; i-- {
						if alist[i].playerID == s.id {
							n := len(alist) - 1
							alist[i] = alist[n]
							alist = alist[:n]
						}
					}
				}
				// close channels to remaining aspirants
				for _, a := range players[s.id] {
					close(a.ch)
				}
				delete(players, s.id)
			}
		}
	}
}

type clientMsg struct {
	ID        playerID
	Name      string
	Index     *uint
	Timestamp uint64
	Symbol    gesture
	err       error
}

type client struct {
	playerID
	conn         *websocket.Conn
	pingTimer    *time.Timer
	msgCh        chan *clientMsg
	reg          *registry
	timeout      time.Duration
	pingInterval time.Duration
	writeTimeout time.Duration
}

func (cl *client) readLoop() {
	defer func() {
		// recover from runtime error due to closed msgCh
		recover()
	}()

	cl.conn.SetReadLimit(64)
	cl.conn.SetPongHandler(func(appData string) error {
		cl.conn.SetReadDeadline(time.Now().Add(cl.timeout))
		return nil
	})
	for {
		var msg clientMsg
		cl.conn.SetReadDeadline(time.Now().Add(cl.timeout))
		msg.err = cl.conn.ReadJSON(&msg)
		cl.msgCh <- &msg
		if msg.err != nil {
			break
		}
	}
}

func (r *registry) handleClient(c *websocket.Conn) {
	// const writeTimeout = time.Second * 10
	const timeout = time.Second * 3
	const pingInterval = timeout * 2 / 3

	cl := client{
		conn:         c,
		pingTimer:    time.NewTimer(pingInterval),
		msgCh:        make(chan *clientMsg, 1),
		reg:          r,
		timeout:      timeout,
		pingInterval: pingInterval,
		writeTimeout: (timeout - pingInterval + 1) / 2,
	}

	log.Printf("new client: %v", c.RemoteAddr())

	defer close(cl.msgCh)

	err := cl.register()
	if err != nil {
		log.Printf("error: %v", err)
	}

	log.Printf("disconnect client: %v", c.RemoteAddr())
}

func (cl *client) writeCandidate(c candidate) error {
	return cl.conn.WriteJSON(struct {
		Index uint
		Name  string
	}{
		Index: c.index,
		Name:  c.name,
	})
}

func (cl *client) register() error {

	var m struct {
		ID        playerID
		Name      string
		Timestamp *uint64
	}
	cl.conn.SetReadDeadline(time.Now().Add(cl.timeout))
	err := cl.conn.ReadJSON(&m)

	if err != nil {
		return err
	}

	if len(m.ID) == 0 || m.Timestamp == nil {
		return fmt.Errorf("unexpected client message: %v", m)
	}

	cl.playerID = m.ID

	s := &seeker{
		id:   m.ID,
		name: m.Name,
		ch:   make(chan []candidate, 1),
	}

	log.Printf("new player: %s (%s)", m.Name, m.ID)

	sel := playerSel{
		id:        m.ID,
		timestamp: timestamp(*m.Timestamp),
		ch:        make(chan interface{}, 1),
		add:       true,
	}

	cl.reg.ch <- sel
	index, others := cl.reg.addSeeker(s)
	defer func() {
		sel.add = false
		cl.reg.ch <- sel
		close(s.ch)
	}()

	// TODO: find better deadline
	cl.conn.SetWriteDeadline(time.Now().Add(cl.timeout))

	err = cl.conn.WriteJSON(struct{ Index uint }{index})
	if err != nil {
		return err
	}

	log.Printf("sending %d candidates", len(others))
	for _, o := range others {
		err = cl.writeCandidate(o)
		if err != nil {
			return err
		}
	}

	log.Printf("waiting for selection")

	return cl.seek(others, s.ch, &sel)
}

func (cl *client) seek(others map[uint]candidate, ch chan []candidate, sel *playerSel) error {
	go cl.readLoop()

	var werr error
loop:
	for werr == nil {
		select {
		case _ = <-cl.pingTimer.C:
			cl.pingTimer.Reset(cl.pingInterval)
			cl.conn.SetWriteDeadline(time.Now().Add(cl.writeTimeout))
			werr = cl.conn.WriteMessage(websocket.PingMessage, []byte{})
		case ol, ok := <-ch:
			if ok {
				for _, o := range ol {
					cl.conn.SetWriteDeadline(time.Now().Add(cl.writeTimeout))
					werr = cl.writeCandidate(o)
					others[o.index] = o
				}
			} else {
				werr = fmt.Errorf("player not available")
				break loop
			}
		case m, ok := <-cl.msgCh:
			if !ok || m.err != nil {
				werr = fmt.Errorf("error reading client message: %v", m.err)
				break loop
			}
			cl.conn.SetWriteDeadline(time.Now().Add(cl.writeTimeout))
			if m.Index != nil {
				o, exists := others[*m.Index]
				if exists {
					sel.selID = o.playerID
					cl.reg.ch <- *sel
				} else {
					errstr := fmt.Sprintf("invalid selection: %d", *m.Index)
					cl.conn.WriteJSON(errorResp{errstr})
					werr = fmt.Errorf(errstr)
				}
				break loop
			} else {
				cl.conn.WriteJSON(errorResp{fmt.Sprintf("invalid message: %v", m)})
				break loop
			}
		}
	}

	if len(sel.selID) > 0 && werr == nil {
		werr = cl.waitForOtherPlayer(sel.ch)
	}
	return werr
}

func (cl *client) waitForOtherPlayer(ch chan interface{}) error {
	const maxSelectionWait = time.Second * 40
	var moveCh chan move
	var err error

	select {
	case _ = <-time.After(maxSelectionWait):
		err = fmt.Errorf("exceeded timeout while waiting for other player")
		cl.conn.SetWriteDeadline(time.Now().Add(cl.writeTimeout))
		cl.conn.WriteJSON(errorResp{err.Error()})
	case s, ok := <-ch:
		cl.conn.SetWriteDeadline(time.Now().Add(cl.writeTimeout))
		if !ok {
			err = fmt.Errorf("player not available")
			cl.conn.WriteJSON(errorResp{err.Error()})
		} else {
			cl.conn.WriteJSON(struct{ Start int }{1})
			moveCh = s.(chan move)
		}
		break
	}

	if moveCh != nil {
		return cl.play(moveCh)
	}

	return err
}

func (cl *client) play(ch chan move) error {
	const maxMoveWait = time.Minute * 3
	select {
	case _ = <-time.After(maxMoveWait):
		err = fmt.Errorf("exceeded timeout while waiting for move")
		cl.conn.SetWriteDeadline(time.Now().Add(cl.writeTimeout))
		cl.conn.WriteJSON(errorResp{err.Error()})
	case m, ok := <-cl.msgCh:
		ch <- move{
			id:        cl.playerID,
			gesture:   m.Symbol,
			timestamp: m.Timestamp,
		}
	}

	return nil
}
