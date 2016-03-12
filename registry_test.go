package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func failOnError(t *testing.T, err error) {
	if err != nil {
		_, file, line, _ := runtime.Caller(1)
		t.Fatalf("%v:%v: %s", file, line, err.Error())
	}
}

func expectEqual(t *testing.T, a, b interface{}, msg string) {
	if !reflect.DeepEqual(a, b) {
		_, file, line, _ := runtime.Caller(1)
		t.Errorf("%v:%v: %v != %v, %s", file, line, a, b, msg)
	}
}

func assert(t *testing.T, v bool, msg string) {
	if !v {
		_, file, line, _ := runtime.Caller(1)
		t.Fatalf("%v:%v: %s", file, line, msg)
	}
}

var reg *registry

func runTestServer(t *testing.T, addr, url string) {
	// TODO: implement restartable server
	if reg == nil {
		reg = newRegistry()
		http.HandleFunc(url, handleConnection(reg))
		go func() { failOnError(t, http.ListenAndServe(addr, nil)) }()
	}
	// reg := newRegistry()
	// http.HandleFunc(url, handleConnection(reg))
	// ln, err := net.Listen("tcp", addr)
	// failOnError(t, err)
	// go func() {
	// 	fmt.Println("starting server")
	// 	failOnError(t, http.Serve(ln, nil))
	// 	fmt.Println("server closed")
	// }()
	// return ln
}

type playerInfo struct {
	index int
	name  string
}

type message struct {
	Index  *int   `json:",omitempty"`
	Name   string `json:",omitempty"`
	Error  string `json:",omitempty"`
	Play   *int   `json:",omitempty"`
	Result *int   `json:",omitempty"`
}

type testclient struct {
	conn      *websocket.Conn
	index     int
	contender []playerInfo
	pongch    chan []byte
	ch        chan message
	synch     chan bool
}

func connect(addr, url string) (*testclient, error) {
	var c *testclient
	conn, _, err := websocket.DefaultDialer.Dial("ws://localhost"+addr+url, nil)

	if err == nil {
		c = &testclient{
			conn:  conn,
			index: -1,
			ch:    make(chan message, 1),
			synch: make(chan bool, 0),
		}
	}

	return c, err
}

func (c *testclient) join(id, name string, timestamp int64) {
	c.conn.WriteJSON(struct {
		Timestamp int64
		ID        string
		Name      string
	}{
		ID: id, Name: name, Timestamp: timestamp,
	})
}

func (c *testclient) selectPlayer(index uint) {
	c.conn.WriteJSON(struct{ Index uint }{Index: index})
}

// func (c *testclient) waitForJoin(timeout time.Duration) error {
// 	var joinResp struct{ Index int }
// 	c.conn.SetReadDeadline(time.Now().Add(timeout))
// 	err := c.conn.ReadJSON(&joinResp)
// 	if err == nil {
// 		c.index = joinResp.Index
// 	}
// 	return err
// }

func (c *testclient) read(t *testing.T, timeout time.Duration, id string) {
	// TODO: handle disconnection
	for {
		var msg message
		c.conn.SetReadDeadline(time.Now().Add(timeout))
		err := c.conn.ReadJSON(&msg)
		_, closed := err.(*websocket.CloseError)
		if closed {
			break
		} else if err != nil {
			t.Errorf("%s: failed to read message: %v(%T)", id, err, err)
			break
		}
		s, _ := json.Marshal(msg)
		log.Printf("%s: msg (%v)", id, string(s))
		c.ch <- msg
	}
	log.Printf("%s: closed read loop\n", id)
	c.synch <- true
}

func (c *testclient) processMessage(msg message) error {
	if len(msg.Error) > 0 {
		return fmt.Errorf("received error: %s", msg.Error)
	} else if msg.Index != nil && len(msg.Name) > 0 {
		c.contender = append(c.contender, playerInfo{name: msg.Name, index: *msg.Index})
	} else if msg.Index != nil && c.index < 0 {
		c.index = *msg.Index
	} else if msg.Index != nil {
		// TODO: remove contender
	}
	return nil
}

func (c *testclient) dispatch() error {
	select {
	case msg := <-c.ch:
		return c.processMessage(msg)
	case <-time.After(time.Second * 6):
		return fmt.Errorf("timeout")
	}
}

//
// func (c *testclient) processAllMessages() error {
// 	for {
// 		select {
// 		case msg := <-c.ch:
// 		default
// 		}
// 	}
// }

func NoTestRendezvous(t *testing.T) {
	addr := ":8080"
	url := "/pepasi"

	runTestServer(t, addr, url)

	wait := time.Millisecond * 10
retry:
	// connect player1
	c1, err := connect(addr, url)
	if err != nil && wait < time.Second*5 {
		wait *= 2
		log.Printf("%v, %s, retrying in %v", err, reflect.TypeOf(err), wait)
		time.Sleep(wait)
		goto retry
	} else if err != nil {
		t.Fatal(err)
	}

	go c1.read(t, time.Second*6, "p1")
	c1.join("1324", "player1", 42)
	err = c1.dispatch()
	if err != nil {
		t.Fatal(err)
	}
	if c1.index != 0 {
		t.Fatalf("expected index 0, got %v", c1.index)
	}

	// connect player2
	c2, err := connect(addr, url)
	if err != nil {
		t.Fatal(err)
	}
	go c2.read(t, time.Second*3, "p2")
	c2.join("2342", "player2", 23)
	err = c2.dispatch()
	if err != nil {
		t.Fatal(err)
	}
	if c2.index != 1 {
		t.Fatalf("expected index 1, got %v", c2.index)
	}

	// player1 receives player2
	err = c1.dispatch()
	if err != nil {
		t.Fatal(err)
	}
	if len(c1.contender) != 1 || (c1.contender[0] != playerInfo{index: 1, name: "player2"}) {
		t.Errorf("exptected player2 as contender, got %v", c1.contender)
	}

	// player2 receive player1
	err = c2.dispatch()
	if err != nil {
		t.Fatal(err)
	}
	if len(c2.contender) != 1 || (c2.contender[0] != playerInfo{index: 0, name: "player1"}) {
		t.Errorf("exptected player1 as contender, got %v", c2.contender)
	}

	// player3 connects
	c3, err := connect(addr, url)
	if err != nil {
		t.Fatal(err)
	}

	go c3.read(t, time.Second*3, "p1")
	c3.join("0000", "player3", 5)
	err = c3.dispatch()
	if err != nil {
		t.Fatal(err)
	}
	if c3.index != 2 {
		t.Fatalf("expected index 2, got %v", c1.index)
	}

	// player3 receives player1 and player2
	if err = c3.dispatch(); err != nil {
		t.Fatal(err)
	}
	if err = c3.dispatch(); err != nil {
		t.Fatal(err)
	}
	if len(c3.contender) != 2 || (c3.contender[0] != playerInfo{index: 0, name: "player1"}) || (c3.contender[1] != playerInfo{index: 1, name: "player2"}) {
		t.Errorf("exptected player1 and player2 as contenders, got %v", c3.contender)
	}

	// player1 receives player3
	err = c1.dispatch()
	if err != nil {
		t.Fatal(err)
	}
	if len(c1.contender) != 2 || (c1.contender[1] != playerInfo{index: 2, name: "player3"}) {
		t.Errorf("exptected player3 as contender, got %v", c1.contender)
	}

	// player2 receives player3
	err = c2.dispatch()
	if err != nil {
		t.Fatal(err)
	}
	if len(c2.contender) != 2 || (c2.contender[1] != playerInfo{index: 2, name: "player3"}) {
		t.Errorf("exptected player3 as contender, got %v", c2.contender)
	}
}

func connectPlayer(t *testing.T, addr, url, id, name string, timestamp int64) *testclient {
	wait := time.Millisecond * 10
retry:
	c, err := connect(addr, url)
	if err != nil && wait < time.Second*3 {
		wait *= 2
		log.Printf("%v, %s, retrying in %v", err, reflect.TypeOf(err), wait)
		time.Sleep(wait)
		goto retry
	} else if err != nil {
		t.Fatal(err)
	}

	go c.read(t, time.Second*1, id)
	c.join(id, name, timestamp)
	err = c.dispatch()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPlay(t *testing.T) {
	addr := ":8080"
	url := "/pepasi"

	runTestServer(t, addr, url)

	c1 := connectPlayer(t, addr, url, "1324", "player1", 42)
	c2 := connectPlayer(t, addr, url, "2342", "player2", 23)

	assert(t, c1.index == 0 && c2.index == 1, "expected index 0 and 1")

	// player1 receives player2
	failOnError(t, c1.dispatch())
	expectEqual(t, len(c1.contender), 1, "exptected player2 as contender")

	// player2 receive player1
	failOnError(t, c2.dispatch())
	expectEqual(t, len(c2.contender), 1, "exptected player1 as contender")

	c1.selectPlayer(1)
	c2.selectPlayer(0)

	failOnError(t, c1.dispatch())
	failOnError(t, c2.dispatch())

	failOnError(t, c2.conn.Close())
	<-c2.synch

	failOnError(t, c1.dispatch())

	failOnError(t, c1.conn.Close())
	<-c1.synch
}
