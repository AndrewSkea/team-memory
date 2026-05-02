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
	log.Fatal(http.ListenAndServe(addr, withCORS(srv.Handler())))
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if originAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func originAllowed(o string) bool {
	if o == "" || o == "null" {
		return true // file://
	}
	// localhost on any port
	return len(o) >= 16 && o[:16] == "http://localhost" ||
		len(o) >= 16 && o[:16] == "http://127.0.0.1"
}
