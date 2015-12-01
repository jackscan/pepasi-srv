package main

import (
	// "github.com/boltdb/bolt"
	"log"
	"net/http"
)

// func handleSync(db *bolt.DB) http.HandlerFunc {
// 	return func(w http.ResponseWriter, r *http.Request) {
// 		query := r.URL.Query()
// 		watchtok := query.Get("tok")

// 	}
// }

// const (
// 	DISCONNECTED = state(iota)
// 	CONNECTED
// 	JOINING
// 	WAITING
// 	PLAYING
// )

func main() {

	// db, err := bolt.Open(dbpath, 0600, nil)

	addr := ":8080"
	url := "/pepasi"
	reg := newRegistry()
	go reg.run()
	http.HandleFunc(url, handleConnection(reg))
	log.Fatal(http.ListenAndServe(addr, nil))
}
