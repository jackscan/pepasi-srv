package main

import (
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

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

type seeker struct {
	id    playerID
	name  string
	index int
	selID playerID
	timestamp
	// channel for player to receive moveCh and results
	resultCh    chan interface{}
	candidateCh chan []candidate
	aspirants   []playerID
	add         bool
}

type registry struct {
	mutex   sync.RWMutex
	seeker  map[playerID]*seeker
	exposed []*seeker
}

func newRegistry() *registry {
	return &registry{seeker: make(map[playerID]*seeker, 2)}
}

func handleConnection(reg *registry) func(http.ResponseWriter, *http.Request) {
	var upgrader websocket.Upgrader
	upgrader.CheckOrigin = func(r *http.Request) bool {
		o := r.Header.Get("Origin")
		if o == "" || o == "file://" {
			return true
		}

		u, err := url.Parse(o)
		if err != nil {
			log.WithField("origin", o).Error(err)
			return false
		}

		if u.Host != r.Host {
			log.WithFields(log.Fields{
				"origin": o,
				"host":   r.Host,
			}).Error("origin does not match host")
			return false
		}

		return true
	}

	return func(w http.ResponseWriter, r *http.Request) {
		log.WithFields(log.Fields{
			"origin": r.Header.Get("Origin"),
			"host":   r.Host,
		}).Info("new connection")

		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Print(err)
			return
		}
		defer c.Close()
		reg.handleClient(c)
	}
}

func (s *seeker) notify(c *candidate) {
	select {
	case s.candidateCh <- []candidate{*c}:
	case l := <-s.candidateCh:
		s.candidateCh <- append(l, *c)
	default:
		log.WithFields(log.Fields{
			"player":    s.id,
			"name":      s.name,
			"candidate": c.playerID,
			"index":     s.index,
			"cname":     c.name,
		}).Fatal("missed candidate")
	}
}

func (r *registry) removeExposedLocked(s *seeker) {
	if s.index >= 0 {
		// notify others
		c := candidate{index: uint(s.index)}
		for _, o := range r.exposed {
			if o != nil && s.selID != o.id {
				o.notify(&c)
			}
		}
		r.exposed[s.index] = nil
		s.index = -1
		close(s.candidateCh)
	}
}

func (r *registry) removeSeekerLocked(s *seeker) {
	// close all other aspirants
	for _, a := range s.aspirants {
		if s.selID != a {
			o := r.seeker[a]
			if o != nil && o.resultCh != nil {
				close(o.resultCh)
				o.resultCh = nil
			}
		}
	}

	// unset selection to remove from all clients
	s.selID = ""
	r.removeExposedLocked(s)
	if s.resultCh != nil {
		close(s.resultCh)
		s.resultCh = nil
	}
	delete(r.seeker, s.id)
}

func (r *registry) removeSeeker(s *seeker) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.removeSeekerLocked(s)
}

func (r *registry) addSeeker(id playerID, name string) *seeker {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	rlog := log.WithFields(log.Fields{
		"player": id,
		"name":   name,
	})

	if os, exists := r.seeker[id]; exists {
		rlog.Warn("remove old seeker")
		r.removeSeekerLocked(os)
	}

	index := -1
	// reserve for current seekers and coming
	others := make([]candidate, 0, len(r.seeker)*2+1)

	// find free index
	for i, o := range r.exposed {
		if o == nil {
			index = i
			break
		}
	}

	// no free index found?
	if index < 0 {
		// append
		index = len(r.exposed)
		r.exposed = append(r.exposed, nil)
	}

	rlog.WithField("index", index).Info("registered")

	nc := candidate{
		index:    uint(index),
		name:     name,
		playerID: id,
	}

	// notify others
	for i, o := range r.exposed {
		if o != nil {
			o.notify(&nc)
			others = append(others, candidate{
				index:    uint(i),
				name:     o.name,
				playerID: o.id,
			})
		}
	}

	s := &seeker{
		id:          id,
		name:        name,
		index:       index,
		candidateCh: make(chan []candidate, 1),
		resultCh:    make(chan interface{}, 1),
	}

	r.seeker[id] = s
	r.exposed[index] = s

	s.candidateCh <- others

	return s
}

func (r *registry) seekerSelect(s *seeker, id playerID) bool {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	rlog := log.WithFields(log.Fields{
		"player": s.id,
		"name":   s.name,
	})

	rlog.WithField("selection", id).Info("selected")

	o := r.seeker[id]

	if o == nil {
		// selected player is already gone
		return false
	}

	s.selID = id

	for _, oid := range r.seeker[s.id].aspirants {
		if oid == id {
			rlog.Info("selection matched")
			// other player is already waiting
			a := player{o.id, o.resultCh}
			b := player{s.id, s.resultCh}
			startCompetition(a, b, s.timestamp-o.timestamp)
			return true
		}
	}

	o.aspirants = append(o.aspirants, s.id)
	r.removeExposedLocked(s)
	return true
}

type clientMsg struct {
	ID        playerID
	Name      string
	Index     *uint
	Timestamp int64
	Symbol    gesture
	err       error
}

type client struct {
	playerID
	conn         *websocket.Conn
	pingTimer    *time.Timer
	msgCh        chan *clientMsg
	writeCh      chan interface{}
	moveCh       chan move
	resultCh     chan interface{}
	reg          *registry
	timeout      time.Duration
	pingInterval time.Duration
	writeTimeout time.Duration
	log          *log.Entry
	syncCh       chan bool
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

	cl.log.Info("read loop end")
}

func (cl *client) writeLoop() {
loop:
	for {
		select {
		case _ = <-cl.pingTimer.C:
			cl.pingTimer.Reset(cl.pingInterval)
			cl.conn.SetWriteDeadline(time.Now().Add(cl.writeTimeout))
			err := cl.conn.WriteMessage(websocket.PingMessage, []byte{})
			if err != nil {
				cl.log.Errorf("failed to ping client: %v", err)
				break loop
			}
		case m, ok := <-cl.writeCh:
			if ok {
				cl.conn.SetWriteDeadline(time.Now().Add(cl.writeTimeout))
				err := cl.conn.WriteJSON(m)
				if err != nil {
					cl.log.Errorf("failed to write message %v: %v", m, err)
					break loop
				}
			} else {
				break loop
			}
		}
	}

	cl.syncCh <- true

	cl.log.Info("write loop end")
}

func (r *registry) handleClient(c *websocket.Conn) {
	// const writeTimeout = time.Second * 10
	const timeout = time.Second * 3
	const pingInterval = timeout * 2 / 3

	cl := client{
		conn:         c,
		pingTimer:    time.NewTimer(pingInterval),
		msgCh:        make(chan *clientMsg, 1),
		writeCh:      make(chan interface{}, 1),
		syncCh:       make(chan bool, 0),
		reg:          r,
		timeout:      timeout,
		pingInterval: pingInterval,
		writeTimeout: (timeout - pingInterval + 1) / 2,
		log:          log.WithFields(log.Fields{"address": c.RemoteAddr()}),
	}

	cl.log.Info("new client")

	defer func() {
		close(cl.msgCh)
		if cl.moveCh != nil {
			close(cl.moveCh)
		}
		close(cl.writeCh)
		<-cl.syncCh
	}()

	err := cl.register()
	if err == nil {
		cl.play()
	} else {
		cl.log.Error(err.Error())
	}

	cl.log.Info("disconnect")
}

func (cl *client) writeCandidate(c candidate) {
	if len(c.name) > 0 {
		cl.writeCh <- struct {
			Index uint
			Name  string
		}{
			Index: c.index,
			Name:  c.name,
		}
	} else {
		cl.writeCh <- struct{ Index uint }{Index: c.index}
	}
}

func (cl *client) register() error {

	var m struct {
		ID        playerID
		Name      string
		Timestamp *int64
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
	if len(m.Name) == 0 {
		m.Name = "Player"
	}

	cl.log = cl.log.WithFields(log.Fields{"player": cl.playerID, "name": m.Name})
	cl.log.Info("registering")
	delete(cl.log.Data, "address")

	s := cl.reg.addSeeker(m.ID, m.Name)
	defer cl.reg.removeSeeker(s)

	// TODO: find better deadline
	cl.conn.SetWriteDeadline(time.Now().Add(cl.timeout))

	err = cl.conn.WriteJSON(struct{ Index int }{s.index})
	if err != nil {
		return err
	}

	err = cl.seek(s)

	if err == nil && len(s.selID) > 0 {
		err = cl.waitForOtherPlayer(s)
	}

	return err
}

func (cl *client) seek(s *seeker) error {
	const maxSelectionWait = time.Second * 40

	go cl.readLoop()
	go cl.writeLoop()

	var werr error

	cl.log.Info("seeking")
	others := make(map[uint]candidate)
loop:
	for werr == nil {
		select {
		case _ = <-time.After(maxSelectionWait):
			werr = fmt.Errorf("exceeded timeout while waiting for selection")
			cl.writeCh <- errorResp{"timeout"}
		case ol, ok := <-s.candidateCh:
			if ok {
				for _, o := range ol {
					cl.writeCandidate(o)
					if len(o.playerID) > 0 {
						others[o.index] = o
					} else {
						delete(others, o.index)
					}
				}
			} else {
				// XXX: channel closed by registry.run()
				// when selected player is not available anymore?
				// can this ever happen? We have not selected anyone yet.
				werr = fmt.Errorf("player not available anymore")
				break loop
			}
		case m, ok := <-cl.msgCh:
			if !ok || m.err != nil {
				werr = fmt.Errorf("error reading client message: %v", m.err)
				break loop
			}
			if m.Index != nil {
				o, exists := others[*m.Index]
				if exists {
					cl.reg.seekerSelect(s, o.playerID)
				} else {
					werr = fmt.Errorf("invalid selection: %d", *m.Index)
					cl.writeCh <- errorResp{werr.Error()}
				}
				break loop
			} else {
				werr = fmt.Errorf("invalid message: %v", m)
				cl.writeCh <- errorResp{werr.Error()}
				break loop
			}
		}
	}

	return werr
}

func (cl *client) waitForOtherPlayer(s *seeker) error {
	const maxSelectionWait = time.Second * 40
	var err error

	cl.log.Info("waiting for other player")

	select {
	case _ = <-time.After(maxSelectionWait):
		err = fmt.Errorf("exceeded timeout while waiting for other player")
		cl.writeCh <- errorResp{"timeout"}
	case ch, ok := <-s.resultCh:
		if !ok {
			err = fmt.Errorf("player not available")
			cl.writeCh <- errorResp{err.Error()}
		} else {
			cl.writeCh <- struct{ Play int }{1}
			cl.moveCh = ch.(chan move)
			cl.resultCh, s.resultCh = s.resultCh, nil
		}
		break
	}

	return err
}

func (cl *client) play() {
	const maxIdle = time.Minute * 3

	defer func() {
		cl.moveCh <- move{gesture: leaveGesture}
	}()

	cl.log.Info("playing")

loop:
	for {
		select {
		case _ = <-time.After(maxIdle):
			cl.log.Warn("disconnecting idle player")
			cl.writeCh <- errorResp{"timeout"}
			break loop
		case m, ok := <-cl.msgCh:
			if ok && m.err == nil {
				cl.log.WithFields(log.Fields{
					"symbol":    m.Symbol,
					"timestamp": m.Timestamp,
				}).Info("move")
				cl.moveCh <- move{
					gesture:   m.Symbol,
					timestamp: timestamp(m.Timestamp),
				}
			} else {
				if _, ok = m.err.(*websocket.CloseError); ok {
					cl.log.Info("connection closed by client")
				} else {
					cl.log.Errorf("%s", m.err)
				}
				break loop
			}
		case r, ok := <-cl.resultCh:
			if ok {
				cl.log.WithField("result", r).Info("turn end")
				cl.writeCh <- struct{ Result int }{r.(int)}
			} else {
				// send end message
				cl.log.Info("match end")
				cl.writeCh <- struct{ Play int }{0}
				break loop
			}
		}
	}
}
