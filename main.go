package main

import (
	"net/http"

	"github.com/boltdb/bolt"

	log "github.com/sirupsen/logrus"
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

	tolerance := timestamp(300)
	addr := ":8080"
	url := "/pepasi"
	dbpath := "pepasi.db"

	db, err := bolt.Open(dbpath, 0600, nil)
	if err != nil {
		log.Fatalf("failed to open %s: %s", dbpath, err.Error())
	}
	defer db.Close()

	reg := newRegistry(db, config{tolerance: tolerance})
	http.HandleFunc(url, handleConnection(reg))
	http.Handle("/download/", http.StripPrefix("/download/", http.FileServer(http.Dir("download"))))
	http.Handle("/config/", http.StripPrefix("/config/", http.FileServer(http.Dir("config-web"))))
	http.Handle("/client/", http.StripPrefix("/client/", http.FileServer(http.Dir("web"))))
	log.Fatal(http.ListenAndServe(addr, nil))
}
