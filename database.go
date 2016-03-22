package main

import (
	"bytes"
	"encoding/binary"
	"fmt"

	log "github.com/sirupsen/logrus"

	"github.com/boltdb/bolt"
)

var (
	dbKeyCompetitions = []byte("Competitions")
	dbKeyUsers        = []byte("Users")
)

func initDatabase(db *bolt.DB) {
	err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(dbKeyUsers)
		if err != nil {
			return fmt.Errorf("failed to create users: %s", err)
		}
		_, err = tx.CreateBucketIfNotExists(dbKeyCompetitions)
		if err != nil {
			return fmt.Errorf("failed to create competitions: %s", err)
		}
		return nil
	})

	log.Fatalf("failed to initialize database: %s", err)
}

type stock struct {
	rocks    uint32
	papers   uint32
	scissors uint32
}

// Overall player stats
type playerStats struct {
	stock
	numMatches uint32
	wins       uint32
	losses     uint32
}

type competitionStats struct {
	stockA     stock
	stockB     stock
	pointsA    uint32
	pointsB    uint32
	numMatches uint32
}

func loadCompetitionStats(db *bolt.DB, playerA, playerB playerID) competitionStats {
	stats := competitionStats{}
	err := db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(dbKeyCompetitions)

		d := c.Get([]byte(fmt.Sprint(playerB, '.', playerA)))
		if d == nil {
			d = c.Get([]byte(fmt.Sprint(playerA, '.', playerB)))
		}

		if d != nil {
			return binary.Read(bytes.NewReader(d), binary.LittleEndian, &stats)
		}

		return nil
	})

	if err != nil {
		log.Errorf("failed to load competition stats: %s", err)
	}

	return stats
}

func loadPlayerStats(db *bolt.DB, id playerID) playerStats {
	stats := playerStats{}
	err := db.View(func(tx *bolt.Tx) error {
		users := tx.Bucket(dbKeyUsers)

		d := users.Get([]byte(id))
		if d != nil {
			return binary.Read(bytes.NewReader(d), binary.LittleEndian, &stats)
		}

		return nil
	})

	if err != nil {
		log.WithField("player", id).
			Errorf("failed to get player stats: %s", err)
	}

	return stats
}

func saveCompetitionStats(db *bolt.DB, playerA, playerB playerID, stats competitionStats) error {
	return db.Update(func(tx *bolt.Tx) error {

		c := tx.Bucket(dbKeyCompetitions)
		key := []byte(fmt.Sprint(playerB, '.', playerA))
		var d bytes.Buffer

		err := binary.Write(&d, binary.LittleEndian, &stats)
		if err != nil {
			return err
		}

		return c.Put(key, d.Bytes())
	})
}

func savePlayerStats(db *bolt.DB, id playerID, stats playerStats) error {
	return db.Update(func(tx *bolt.Tx) error {
		users := tx.Bucket(dbKeyUsers)
		key := []byte(id)
		var d bytes.Buffer
		err := binary.Write(&d, binary.LittleEndian, &stats)
		if err != nil {
			return err
		}

		return users.Put(key, d.Bytes())
	})
}
