package main

import (
	"log"
	"sync"

	"github.com/getlantern/systray"
)

var (
	mStatus    *systray.MenuItem
	mLastID    *systray.MenuItem
	mAutoStart *systray.MenuItem

	readerSlots [maxReaderSlots]*systray.MenuItem

	slotMu    sync.Mutex
	slotNames [maxReaderSlots]string
)

func setStatus(text string) {
	log.Printf("[tapbridge] %s", text)
	if mStatus != nil {
		mStatus.SetTitle(text)
	}
}

func setLastScanned(id string) {
	if mLastID != nil {
		mLastID.SetTitle("Last scanned: " + id)
	}
}

func buildTray(cfg Config) {
	systray.SetIcon(iconData)
	systray.SetTooltip("TapBridge")

	mStatus = systray.AddMenuItem("Starting...", "")
	mStatus.Disable()

	mLastID = systray.AddMenuItem("Last scanned: (none)", "")
	mLastID.Disable()

	systray.AddSeparator()

	mPort := systray.AddMenuItem("ws://localhost:"+cfg.Port, "")
	mPort.Disable()

	readerMenu := systray.AddMenuItem("Select Reader", "Choose which connected reader to use")
	for i := 0; i < maxReaderSlots; i++ {
		slot := readerMenu.AddSubMenuItemCheckbox("", "", false)
		slot.Hide()
		readerSlots[i] = slot
		go watchReaderSlot(i, slot)
	}

	systray.AddSeparator()

	mAutoStart = systray.AddMenuItemCheckbox("Start at Login", "Launch TapBridge automatically", isAutoStartEnabled())
	go watchAutoStart(mAutoStart)

	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit TapBridge", "")

	go func() {
		<-mQuit.ClickedCh
		systray.Quit()
	}()
}

func watchReaderSlot(i int, slot *systray.MenuItem) {
	for range slot.ClickedCh {
		slotMu.Lock()
		name := slotNames[i]
		slotMu.Unlock()
		if name == "" {
			continue
		}
		setSelectedReader(name)
	}
}

func watchAutoStart(item *systray.MenuItem) {
	for range item.ClickedCh {
		if item.Checked() {
			if err := disableAutoStart(); err != nil {
				log.Printf("[tapbridge] disable auto-start failed: %v", err)
				continue
			}
			item.Uncheck()
			setAutoStart(false)
			setStatus("Start at Login disabled")
		} else {
			if err := enableAutoStart(); err != nil {
				log.Printf("[tapbridge] enable auto-start failed: %v", err)
				setStatus("Could not enable Start at Login")
				continue
			}
			item.Check()
			setAutoStart(true)
			setStatus("Start at Login enabled")
		}
	}
}

// updateReaderMenu reflects the currently connected readers and the active
// selection (mirroring pickReader's fallback-to-first-reader behavior) in the
// tray's "Select Reader" submenu.
func updateReaderMenu(readers []string, selected string) {
	slotMu.Lock()
	defer slotMu.Unlock()

	foundSelected := false
	if selected != "" {
		for _, r := range readers {
			if r == selected {
				foundSelected = true
				break
			}
		}
	}

	for i := 0; i < maxReaderSlots; i++ {
		slot := readerSlots[i]
		if i >= len(readers) {
			slotNames[i] = ""
			slot.Hide()
			slot.Uncheck()
			continue
		}
		name := readers[i]
		slotNames[i] = name
		slot.SetTitle(name)
		slot.Show()
		if name == selected || (!foundSelected && i == 0) {
			slot.Check()
		} else {
			slot.Uncheck()
		}
	}
}
