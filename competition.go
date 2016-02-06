package main

import (
	"fmt"

	log "github.com/sirupsen/logrus"
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
	gesture
	timestamp
}

// type competition struct {
// 	a, b *player
// 	dt   timestamp
// 	ch   chan move
// }

func startCompetition(a, b player, dt timestamp) {
	ach := make(chan move, 1)
	bch := make(chan move, 1)

	a.ch <- ach
	b.ch <- bch

	go func() {
		var ma, mb move
		var ok bool
		defer close(a.ch)
		defer close(b.ch)
	loop:
		for {
			select {
			case ma, ok = <-ach:
			case mb, ok = <-bch:
			}

			if !ok || ma.gesture == leaveGesture || mb.gesture == leaveGesture {
				break loop
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
