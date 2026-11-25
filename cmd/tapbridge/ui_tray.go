//go:build !notray

package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/getlantern/systray"

	"github.com/felaris/tapbridge/assets"
	"github.com/felaris/tapbridge/internal/autostart"
	"github.com/felaris/tapbridge/internal/config"
	"github.com/felaris/tapbridge/internal/logging"
	"github.com/felaris/tapbridge/internal/tray"
)

// runUI shows the system tray, or runs headless if asked to.
func runUI(a *app, headless bool) {
	if headless {
		runHeadless(a)
		return
	}
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		systray.Quit()
	}()
	systray.Run(func() { a.start(newTray(a)) }, func() {})
}

func newTray(a *app) *tray.Tray {
	return tray.New(tray.Options{
		Icon:      assets.Icon,
		Version:   version,
		URL:       "ws://localhost:" + a.store.Get().Port,
		LogPath:   a.logPath,
		AutoStart: autostart.IsEnabled(),
		OnSelectReader: func(name string) {
			if err := a.store.Update(func(c *config.Config) { c.SelectedReader = name }); err != nil {
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
				_ = a.store.Update(func(c *config.Config) { c.AutoStart = enable })
			}
			return err
		},
		OnOpenLog: func() {
			if err := logging.Open(a.logPath); err != nil {
				log.Printf("[tapbridge] couldn't open the log file: %v", err)
			}
		},
	})
}
