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

	mLog := systray.AddMenuItem("Open Log File", "Show TapBridge's diagnostic log")
	go func() {
		for range mLog.ClickedCh {
			if p := logFilePath(); p != "" {
				if err := openPath(p); err != nil {
					log.Printf("[tapbridge] open log failed: %v", err)
				}
			}
		}
	}()

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

// updateReaderMenuIfChanged calls updateReaderMenu only when the reader list
// or selection actually changed. Rewriting tray items on every poll makes the
// Windows tray menu flicker and close itself while the user has it open.
func updateReaderMenuIfChanged(lastReaders *[]string, lastSelected *string, readers []string, selected string) {
	if selected == *lastSelected && equalStrings(readers, *lastReaders) {
		return
	}
	*lastReaders = append([]string(nil), readers...)
	*lastSelected = selected
	updateReaderMenu(readers, selected)
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// updateReaderMenu reflects the currently connected readers and the active
// selection (using pickReader so the checkmark matches the reader actually polled) in the
// tray's "Select Reader" submenu.
func updateReaderMenu(readers []string, selected string) {
	slotMu.Lock()
	defer slotMu.Unlock()

	active := pickReader(readers, selected)

	for i := 0; i < maxReaderSlots; i++ {
		slot := readerSlots[i]
		if slot == nil {
			continue
		}
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
		if name == active {
			slot.Check()
		} else {
			slot.Uncheck()
		}
	}
}
