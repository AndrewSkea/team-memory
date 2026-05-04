package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/AndrewSkea/team-memory/mcp/server"
)

func main() {
	port := flag.String("port", "7438", "loopback port")
	flag.Parse()

	srv := server.New(server.Config{})
	addr := "127.0.0.1:" + *port
	log.Printf("team-memory-mcp listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, server.WithCORS(srv.Handler())))
}
