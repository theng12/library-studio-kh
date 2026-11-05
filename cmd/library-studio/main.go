// Command library-studio runs the Library Studio KH dashboard on this computer.
// The desktop shell starts it and opens the address it prints; it also runs on its own.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"librarystudio/internal/config"
	"librarystudio/internal/server"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "address to listen on (this computer only)")
	flag.Parse()

	home, err := config.Dir()
	if err != nil {
		log.Fatal(err)
	}
	store, err := config.Open(home)
	if err != nil {
		log.Fatal(err)
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	if h, _, _ := net.SplitHostPort(ln.Addr().String()); !net.ParseIP(h).IsLoopback() {
		log.Fatal("Library Studio KH only listens on this computer (127.0.0.1)")
	}
	s := server.New(store, home)
	go func() {
		for {
			s.RunCleanup()
			time.Sleep(time.Hour)
		}
	}()
	// The desktop shell reads this exact line.
	fmt.Printf("LISTENING http://%s/#%s\n", ln.Addr(), s.Token)
	log.Fatal((&http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}).Serve(ln))
}
