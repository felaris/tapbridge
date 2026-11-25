// Command tapbridge bridges a USB PC/SC NFC reader to web pages over a local
// WebSocket. It normally runs as a system tray app; with --headless (or when
// built with -tags notray, as in the Docker image) it runs without a GUI.
//
// See https://github.com/felaris/tapbridge.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/felaris/tapbridge/internal/config"
	"github.com/felaris/tapbridge/internal/logging"
	"github.com/felaris/tapbridge/internal/reader"
	"github.com/felaris/tapbridge/internal/server"
)

// version is set at build time with -ldflags "-X main.version=v1.2.3".
var version = "dev"

// UI is what the core reports status to: the tray menu, or the log when
// running headless.
type UI interface {
	SetStatus(text string)
	SetLastScanned(id string)
	SetReaders(readers []string, active string)
}

// app holds what both front-ends need.
type app struct {
	store   *config.Store
	logPath string
}

func main() {
	var (
		host        = flag.String("host", "", "address to bind (default 127.0.0.1, loopback only; 0.0.0.0 exposes card data to your network)")
		port        = flag.String("port", "", "WebSocket port (default 8765)")
		origins     = flag.String("allow-origin", "", "comma-separated browser origins allowed to connect (default: localhost and 127.0.0.1 only)")
		readerName  = flag.String("reader", "", "name of the reader to use (default: pick automatically)")
		headless    = flag.Bool("headless", envBool("TAPBRIDGE_HEADLESS"), "run without the system tray (for servers and containers)")
		healthcheck = flag.Bool("healthcheck", false, "exit 0 if a TapBridge is answering on the configured port, 1 otherwise")
		showVersion = flag.Bool("version", false, "print the version and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("tapbridge", version)
		return
	}

	cfg := config.Load(config.Overrides{Host: *host, Port: *port, Origins: *origins, Reader: *readerName})
	if *healthcheck {
		os.Exit(checkHealth(cfg))
	}

	dir, _ := config.Dir()
	logPath := logging.Setup(dir)
	log.Printf("[tapbridge] TapBridge %s starting (%s/%s)", version, runtime.GOOS, runtime.GOARCH)
	if logPath != "" {
		log.Printf("[tapbridge] log file: %s", logPath)
	}

	a := &app{store: config.NewStore(cfg), logPath: logPath}
	runUI(a, *headless) // blocks until quit
}

// start wires the reader and the WebSocket server to ui. It returns false if
// the server could not start.
func (a *app) start(ui UI) bool {
	cfg := a.store.Get()

	var srv *server.Server
	nfc := reader.New(reader.Events{
		Status:  ui.SetStatus,
		Readers: ui.SetReaders,
		Card: func(id string) {
			ui.SetLastScanned(id)
			srv.Broadcast(server.Msg{Type: server.TypeCard, ID: id})
		},
	}, func() string { return a.store.Get().SelectedReader })

	srv = server.New(server.Options{
		AllowedOrigins:  func() []string { return a.store.Get().AllowedOrigins },
		IsAllowedOrigin: config.IsAllowedOrigin,
		OnWrite: func(id, recordType string, reply func(server.Msg), gone <-chan struct{}) error {
			err := nfc.RequestWrite(reader.WriteRequest{
				ID:         id,
				RecordType: recordType,
				Canceled:   gone,
				Done: func(e *reader.Error) {
					if e != nil {
						reply(server.ErrorMsg(server.TypeWriteError, e))
						return
					}
					reply(server.Msg{Type: server.TypeWriteOK, ID: id})
				},
			})
			if err != nil {
				return reader.Explain(err)
			}
			ui.SetStatus("Ready to write — tap a card")
			return nil
		},
	})

	if err := listen(cfg, srv); err != nil {
		ui.SetStatus(err.Error())
		return false
	}
	go nfc.Run()
	return true
}

// listen starts the WebSocket server. It binds before returning so that a
// port conflict is reported rather than killing the process.
func listen(cfg config.Config, handler http.Handler) error {
	addr := net.JoinHostPort(cfg.Host, cfg.Port)
	if !config.IsLoopbackHost(cfg.Host) {
		log.Printf("[tapbridge] NOTE: listening on %s — reachable from outside this machine unless a firewall or container port mapping restricts it", addr)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return explainListenError(cfg, err)
	}
	log.Printf("[tapbridge] listening on ws://%s", addr)
	go serve(ln, handler)

	// On Windows "localhost" resolves to ::1 first, and some clients (Node
	// 17+, .NET, some WebViews) never fall back to 127.0.0.1. When bound to
	// IPv4 loopback, also listen on IPv6 loopback. Best effort.
	if cfg.Host == "127.0.0.1" || cfg.Host == "localhost" {
		if ln6, err := net.Listen("tcp", net.JoinHostPort("::1", cfg.Port)); err == nil {
			log.Printf("[tapbridge] also listening on ws://[::1]:%s", cfg.Port)
			go serve(ln6, handler)
		}
	}
	return nil
}

func serve(ln net.Listener, handler http.Handler) {
	if err := http.Serve(ln, handler); err != nil {
		log.Printf("[tapbridge] server on %s stopped: %v", ln.Addr(), err)
	}
}

func explainListenError(cfg config.Config, err error) error {
	switch {
	case errors.Is(err, syscall.EADDRINUSE) || isWindowsAddrInUse(err):
		return fmt.Errorf("Port %s is already in use. TapBridge may already be running (check the tray), or another app is using the port — start TapBridge with --port 9000 to use a different one", cfg.Port)
	case errors.Is(err, syscall.EACCES) || errors.Is(err, os.ErrPermission):
		return fmt.Errorf("Not allowed to listen on port %s. Pick a port above 1024 with --port, or allow TapBridge in your firewall/antivirus", cfg.Port)
	}
	return fmt.Errorf("Couldn't start the WebSocket server on %s:%s: %v", cfg.Host, cfg.Port, err)
}

// isWindowsAddrInUse matches WSAEADDRINUSE, which Go does not map to
// syscall.EADDRINUSE on Windows.
func isWindowsAddrInUse(err error) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && errno == 10048
}

// checkHealth reports whether a TapBridge answers on the configured port.
// Used by the Docker HEALTHCHECK, which has no curl in the image.
func checkHealth(cfg config.Config) int {
	host := cfg.Host
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort(host, cfg.Port) + "/")
	if err != nil {
		fmt.Fprintln(os.Stderr, "unhealthy:", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "unhealthy: status", resp.Status)
		return 1
	}
	return 0
}

func envBool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
