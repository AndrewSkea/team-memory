// Command fakegh serves an in-memory fake of the GitHub API for the browser
// end-to-end tests. Not shipped in releases.
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/AndrewSkea/team-memory/mcp/internal/fakegh"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:19439", "listen address")
	owner := flag.String("owner", "acme", "repo owner")
	repo := flag.String("repo", "kb", "repo name")
	token := flag.String("token", "tok", "accepted token")
	login := flag.String("login", "alice", "login returned by /user")
	flag.Parse()
	s := fakegh.New(*owner, *repo, *token)
	s.Login = *login
	log.Printf("fake GitHub for %s/%s on http://%s", *owner, *repo, *addr)
	log.Fatal(http.ListenAndServe(*addr, s))
}
