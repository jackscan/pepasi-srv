package main

import (
	"fmt"
	"log"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func runTestServer(t *testing.T, addr, url string) {
	reg := newRegistry()
	go reg.run()
	http.HandleFunc(url, handleConnection(reg))
	t.Fatal(http.ListenAndServe(addr, nil))
}

type playerInfo struct {
	index int
	name  string
}

type message struct {
	Index int
	Name  string
	Error string
}

type testclient struct {
	conn      *websocket.Conn
	index     int
	contender []playerInfo
	pongch    chan []byte
	ch        chan message
}

func connect(addr, url string) (*testclient, error) {
	var c *testclient
	conn, _, err := websocket.DefaultDialer.Dial("ws://localhost"+addr+url, nil)

	if err == nil {
		c = &testclient{
			conn:  conn,
			index: -1,
			ch:    make(chan message, 1),
		}
	}

	return c, err
}

func (c *testclient) join(id, name string, timestamp uint32) {
	c.conn.WriteJSON(struct {
		Timestamp uint32
		ID        string
		Name      string
	}{
		ID: id, Name: name, Timestamp: timestamp,
	})
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
	for {
		var msg message
		c.conn.SetReadDeadline(time.Now().Add(timeout))
		err := c.conn.ReadJSON(&msg)
		if err != nil {
			t.Errorf("failed to read message: %v", err)
		}
		log.Printf("%s: msg (%v)", id, msg)
		c.ch <- msg
	}
}

func (c *testclient) processMessage(msg message) error {
	if len(msg.Error) > 0 {
		return fmt.Errorf("received error: %s", msg.Error)
	} else if len(msg.Name) > 0 {
		c.contender = append(c.contender, playerInfo{name: msg.Name, index: msg.Index})
	} else {
		c.index = msg.Index
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

func TestRendezvous(t *testing.T) {
	addr := ":8080"
	url := "/pepasi"

	go runTestServer(t, addr, url)

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
