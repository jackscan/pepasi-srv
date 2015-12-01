package main

import (
	"testing"
	"time"
)

func TestClosedChannel(t *testing.T) {
	ch := make(chan bool, 1)

	defer func() {
		if x := recover(); x == nil {
			t.Error("exptected runtime error")
		} else {
			t.Logf("result: %v", x)
		}
	}()

	close(ch)

	ch <- true
}

func TestSelectOnClose(t *testing.T) {
	ch := make(chan bool, 1)
	go close(ch)

loop:
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				break loop
			}
		case _ = <-time.After(time.Second):
			t.Fail()
		}
	}
}

func TestClosure(t *testing.T) {
	b := false

	defer func() {
		if !b {
			t.Fail()
		}
	}()

	b = true
}
