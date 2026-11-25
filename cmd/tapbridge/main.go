// Command tapbridge is a system tray app that bridges a USB PC/SC NFC reader
// to web pages over a local WebSocket. See https://github.com/felaris/tapbridge.
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
	"syscall"

	"github.com/getlantern/systray"

	"github.com/felaris/tapbridge/assets"
	"github.com/felaris/tapbridge/internal/autostart"
	"github.com/felaris/tapbridge/internal/config"
	"github.com/felaris/tapbridge/internal/logging"
	"github.com/felaris/tapbridge/internal/reader"
	"github.com/felaris/tapbridge/internal/server"
	"github.com/felaris/tapbridge/internal/tray"
)

// version is set at build time with -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() {
	var (
		host        = flag.String("host", "", "address to bind (default 127.0.0.1, loopback only; 0.0.0.0 exposes card data to your network)")
		port        = flag.String("port", "", "WebSocket port (default 8765)")
		origins     = flag.String("allow-origin", "", "comma-separated browser origins allowed to connect (default: localhost and 127.0.0.1 only)")
		showVersion = flag.Bool("version", false, "print the version and exit")
	)
	flag.Parse()
	if *showVersion {
		fmt.Println("tapbridge", version)
		return
	}

	dir, _ := config.Dir()
	logPath := logging.Setup(dir)
	log.Printf("[tapbridge] TapBridge %s starting (%s/%s)", version, runtime.GOOS, runtime.GOARCH)
	if logPath != "" {
		log.Printf("[tapbridge] log file: %s", logPath)
	}

	store := config.NewStore(config.Load(config.Overrides{Host: *host, Port: *port, Origins: *origins}))

	systray.Run(func() { start(store, logPath) }, func() {})
}

// start runs once the tray is ready.
func start(store *config.Store, logPath string) {
	cfg := store.Get()

	t := tray.New(tray.Options{
		Icon:      assets.Icon,
		Version:   version,
		URL:       "ws://localhost:" + cfg.Port,
		LogPath:   logPath,
		AutoStart: autostart.IsEnabled(),
		OnSelectReader: func(name string) {
			if err := store.Update(func(c *config.Config) { c.SelectedReader = name }); err != nil {
				log.Printf("[tapbridge] couldn't save reader choice: %v", err)
			}
		},
		OnAutoStart: func(enable bool) error {
			var err error
			if enable {
				err = autostart.Enable()
			} else {
				err = autostart.Disable()
			}
			if err == nil {
				_ = store.Update(func(c *config.Config) { c.AutoStart = enable })
			}
			return err
		},
		OnOpenLog: func() {
			if err := logging.Open(logPath); err != nil {
				log.Printf("[tapbridge] couldn't open the log file: %v", err)
			}
		},
	})

	var srv *server.Server
	nfc := reader.New(reader.Events{
		Status:  t.SetStatus,
		Readers: t.SetReaders,
		Card: func(id string) {
			t.SetLastScanned(id)
			srv.Broadcast(server.Msg{Type: server.TypeCard, ID: id})
		},
	}, func() string { return store.Get().SelectedReader })

	srv = server.New(server.Options{
		AllowedOrigins:  func() []string { return store.Get().AllowedOrigins },
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
			t.SetStatus("Ready to write — tap a card")
			return nil
		},
	})

	if err := listen(cfg, srv); err != nil {
		t.SetStatus(err.Error())
		log.Printf("[tapbridge] %v", err)
		return
	}
	go nfc.Run()
}

// listen starts the WebSocket server. It binds before returning so that a
// port conflict is reported in the tray rather than killing the process.
func listen(cfg config.Config, handler http.Handler) error {
	addr := net.JoinHostPort(cfg.Host, cfg.Port)
	if !config.IsLoopbackHost(cfg.Host) {
		log.Printf("[tapbridge] WARNING: listening on %s exposes card data beyond this computer", addr)
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
