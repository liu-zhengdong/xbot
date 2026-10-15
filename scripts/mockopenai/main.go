package main

import (
	"flag"
	"log"
	"net/http"

	"xbot/internal/mockopenai"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:18765", "listen address")
	dir := flag.String("dir", "", "workspace directory used by the scripted tools")
	flag.Parse()
	log.Printf("mock openai listening on %s", *addr)
	if err := http.ListenAndServe(*addr, mockopenai.NewMux(*dir)); err != nil {
		log.Fatal(err)
	}
}
