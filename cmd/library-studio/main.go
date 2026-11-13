// Command library-studio runs the Library Studio KH dashboard on this computer.
// The desktop shell starts it and opens the address it prints; it also runs on its own.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	librarystudio "librarystudio"
	"librarystudio/internal/config"
	"librarystudio/internal/library"
	"librarystudio/internal/platform"
	"librarystudio/internal/server"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "address to listen on (this computer only)")
	service := flag.Bool("service", false, "run the background host on a fixed loopback port")
	port := flag.Int("port", defaultServicePort, "port for -service (this computer only)")
	withShell := flag.Bool("with-shell", false, "exit when the desktop shell closes our stdin")
	hostControl := flag.String("host-control", "", "manage the macOS background host: install, start, stop or status")
	hostHome := flag.String("host-home", "", "configuration home for -host-control (default: the app's config folder)")
	serverExe := flag.String("exe", "", "trusted local server path for -host-control install (default: this executable)")
	clientHost := flag.String("client-host", "", "use another Library host webpage URL, or clear this desktop's saved host")
	testMode := flag.Bool("test-mode", false, "headless test server on 127.0.0.1 with the isolated KH_TEST_HOME (scripts/test-ui.sh)")
	seed := flag.Bool("seed-fixtures", false, "write the headless UI test fixtures into KH_TEST_HOME; alone, seed and exit")
	flag.Parse()

	// Test mode decides before config.Dir so it can never touch the owner's data.
	if *testMode || *seed {
		runTestMode(*seed, *testMode)
		return
	}

	home, err := config.Dir()
	if err != nil {
		log.Fatal(err)
	}
	if *clientHost != "" {
		out, code := runClientHost(*clientHost, home)
		printClientHost(out)
		os.Exit(code)
	}
	if *hostControl != "" {
		if *hostHome != "" {
			home = *hostHome
		}
		exe := *serverExe
		if exe == "" {
			exe, _ = os.Executable()
		}
		spec := platform.HostServiceSpec{Exe: exe, Home: home, Port: *port}
		out, code := runHostControl(*hostControl, hostControlService(), spec)
		printHostControl(out)
		os.Exit(code)
	}
	if *service {
		runService(home, *port) // never returns
	}
	if *withShell {
		// A saved client host wins before any local attach, server start or
		// owner creation. It is only the normal webpage URL, never a token.
		handled, err := remoteShell(home, os.Stdin, os.Stdout)
		if err != nil {
			log.Fatalf("Library Studio KH could not read this desktop's saved host: %v", err)
		}
		if handled {
			return
		}
		info, err := attach(home)
		if err == nil {
			// The service outlives this helper; the shell only owns the window.
			fmt.Printf("LISTENING http://%s/#%s\n", info.Address, info.Token)
			waitShell(os.Stdin)
			return
		}
		if !errors.Is(err, errNoHost) {
			log.Fatal(err)
		}
		// Only the installed job is started here; it may use a different port
		// or executable than this process's flags.
		spec := platform.HostServiceSpec{Home: home}
		svc := hostControlService()
		st := svc.Status(spec)
		exe, _ := os.Executable()
		if st.Supported && st.Installed {
			// A login job is wanted: start it and wait for its proof instead of
			// silently starting another desktop server.
			spec.Exe = exe
			started, startErr := svc.Start(spec)
			if startErr != nil {
				log.Fatalf("Library Studio KH could not start its background host: %v", startErr)
			}
			info, err = waitForHost(home, 10*time.Second)
			if err != nil {
				if started.State == platform.HostServiceNeedsRestart {
					log.Fatalf("Library Studio KH's background host did not start cleanly. Another Library Studio KH server may already be using %s. Quit it, then reopen Library Studio KH.", home)
				}
				log.Fatalf("Library Studio KH's background host did not become ready: %v", err)
			}
			fmt.Printf("LISTENING http://%s/#%s\n", info.Address, info.Token)
			waitShell(os.Stdin)
			return
		}
		// No background host configured: the old desktop lifecycle.
		go func() { waitShell(os.Stdin); os.Exit(0) }()
	}

	release, err := platform.LockConfigHome(home)
	if err != nil {
		blocked("Another Library Studio KH is already using this Mac's Library folder. Quit it, then open this one again.")
	}
	defer release()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		blocked(listenProblem(*addr, err))
	}
	if h, _, _ := net.SplitHostPort(ln.Addr().String()); !net.ParseIP(h).IsLoopback() {
		log.Fatal("Library Studio KH only listens on this computer (127.0.0.1)")
	}
	s, err := newServer(home, ln.Addr().String())
	if err != nil {
		log.Fatal(err)
	}
	go s.Meta.Watch() // keeps the metadata-reachability line honest on every page
	// The desktop shell reads this exact line.
	fmt.Printf("LISTENING http://%s/#%s\n", ln.Addr(), s.Token)
	log.Fatal(serve(s, ln))
}

// blocked prints one plain line for the desktop shell to show instead of a crash,
// then exits: a taken port or a second copy must never abort the app.
func blocked(reason string) {
	fmt.Printf("BLOCKED %s\n", reason)
	os.Exit(3)
}

func listenProblem(addr string, err error) string {
	if errors.Is(err, syscall.EADDRINUSE) {
		return "Another Library Studio KH is already running on this Mac. Quit it, then open this one again."
	}
	return fmt.Sprintf("Library Studio KH could not start its server on %s: %v", addr, err)
}

// runService is the one background host. It keeps a fixed loopback port and writes
// the private descriptor a desktop shell attaches to. It never prints its token.
func runService(home string, port int) {
	if port < 1024 || port > 65535 {
		log.Fatalf("Background host port must be a whole number from 1024 to 65535, not %d.", port)
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	release, err := platform.LockConfigHome(home)
	if err != nil {
		log.Fatal(err)
	}
	defer release()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		blocked(listenProblem(addr, err))
	}
	s, err := newServer(home, ln.Addr().String())
	if err != nil {
		ln.Close()
		log.Fatal(err)
	}
	go s.Meta.Watch()
	info := runtimeInfo{PID: os.Getpid(), Address: ln.Addr().String(), Token: s.Token, Version: librarystudio.Version()}
	if err := writeRuntime(home, info); err != nil {
		ln.Close()
		log.Fatalf("Background host could not save its private runtime information: %v", err)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		removeRuntime(home, info)
		os.Exit(0)
	}()
	log.Printf("Library Studio KH background host is ready")
	err = serve(s, ln)
	removeRuntime(home, info)
	if err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func newServer(home, localAddress string) (*server.Server, error) {
	s, err := openServer(home, localAddress)
	if err != nil {
		return nil, err
	}
	store := s.Store
	if err := s.ConfigureRemote(); err != nil {
		log.Printf("Tailscale: %v", err)
	}
	go func() {
		for {
			s.RunCleanup()
			time.Sleep(time.Hour)
		}
	}()
	go func() {
		for {
			_ = library.Sample(store.Get(), home, time.Now()) // a missed reading only leaves a gap
			time.Sleep(library.SampleEvery)
		}
	}()
	return s, nil
}

// openServer builds the dashboard server without Tailscale or background loops.
func openServer(home, localAddress string) (*server.Server, error) {
	store, err := config.Open(home)
	if err != nil {
		return nil, err
	}
	s := server.New(store, home)
	s.LocalURL = "http://" + localAddress + "/"
	if err := s.AccessError(); err != nil {
		return nil, err
	}
	return s, nil
}

func serve(s *server.Server, ln net.Listener) error {
	return (&http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}).Serve(ln)
}
