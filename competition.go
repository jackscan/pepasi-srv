package main

import (
	"fmt"
	"log"
)

type timestamp int32
type result int
type gesture int

const (
	noGesture = gesture(iota)
	rockGesture
	paperGesture
	scissorGesture
	leaveGesture
)

type player struct {
	id playerID
	ch chan interface{}
}

type move struct {
	id playerID
	gesture
	timestamp
}

// type competition struct {
// 	a, b *player
// 	dt   timestamp
// 	ch   chan move
// }

func startCompetition(a, b player, dt timestamp) {
	ch := make(chan move, 2)

	a.ch <- ch
	b.ch <- ch

	go func() {
		var ma, mb move
		defer close(a.ch)
		defer close(b.ch)
	loop:
		for {
			select {
			case m, ok := <-ch:
				if !ok || m.gesture == leaveGesture {
					break loop
				}
				if m.id == a.id {
					ma = m
				} else if m.id == b.id {
					mb = m
				} else {
					panic(m)
				}
			}
			if ma.gesture != noGesture && mb.gesture != noGesture {
				switch ma.gesture - mb.gesture {
				case 0: // draw
					a.ch <- 0
					b.ch <- 0
				case 1, -2: // a wins
					a.ch <- 1
					b.ch <- -1
				case 2, -1: // b wins
					a.ch <- -1
					b.ch <- 1
				default:
					panic(fmt.Errorf("unexpected moves: %v, %v", ma, mb))
				}
				ma.gesture = noGesture
				mb.gesture = noGesture
			}
		}
		log.Printf("competition (%s, %s) closed", a.id, b.id)
	}()
}
