// Package tray builds TapBridge's system tray icon and menu.
package tray

import (
	"log"
	"sync"
	"unicode/utf8"

	"github.com/getlantern/systray"
)

// maxReaderSlots is how many readers the "Select Reader" submenu can list.
// systray cannot add items after startup, so slots are pre-created and
// shown/hidden as readers come and go.
const maxReaderSlots = 8

// maxStatusLen keeps long (explained) error messages from producing an
// absurdly wide menu; the full text is in the tooltip and the log.
const maxStatusLen = 90

// Options configures the tray. Callbacks run on their own goroutines.
type Options struct {
	Icon      []byte
	Version   string
	URL       string // shown as a disabled menu item, e.g. ws://localhost:8765
	LogPath   string // enables "Open Log File" when non-empty
	AutoStart bool   // initial checkbox state

	OnSelectReader func(name string)
	OnAutoStart    func(enable bool) error
	OnOpenLog      func()
}

// Tray is a running tray menu. Its methods are safe for concurrent use and
// do nothing before the menu is built.
type Tray struct {
	opts Options

	mStatus *systray.MenuItem
	mLastID *systray.MenuItem

	slotMu    sync.Mutex
	slots     [maxReaderSlots]*systray.MenuItem
	slotNames [maxReaderSlots]string
}

// New builds the tray menu. Call it from systray's onReady callback.
func New(opts Options) *Tray {
	t := &Tray{opts: opts}

	systray.SetIcon(opts.Icon)
	systray.SetTooltip("TapBridge " + opts.Version)

	t.mStatus = systray.AddMenuItem("Starting...", "")
	t.mStatus.Disable()
	t.mLastID = systray.AddMenuItem("Last scanned: (none)", "")
	t.mLastID.Disable()

	systray.AddSeparator()

	mURL := systray.AddMenuItem(opts.URL, "Connect your web app to this address")
	mURL.Disable()

	readerMenu := systray.AddMenuItem("Select Reader", "Choose which connected reader to use")
	for i := range t.slots {
		slot := readerMenu.AddSubMenuItemCheckbox("", "", false)
		slot.Hide()
		t.slots[i] = slot
		go t.watchSlot(i, slot)
	}

	systray.AddSeparator()

	mAuto := systray.AddMenuItemCheckbox("Start at Login", "Launch TapBridge automatically", opts.AutoStart)
	go t.watchAutoStart(mAuto)

	if opts.LogPath != "" && opts.OnOpenLog != nil {
		mLog := systray.AddMenuItem("Open Log File", opts.LogPath)
		go func() {
			for range mLog.ClickedCh {
				opts.OnOpenLog()
			}
		}()
	}

	systray.AddSeparator()
	mVersion := systray.AddMenuItem("TapBridge "+opts.Version, "")
	mVersion.Disable()
	mQuit := systray.AddMenuItem("Quit TapBridge", "")
	go func() {
		<-mQuit.ClickedCh
		systray.Quit()
	}()

	return t
}

// SetStatus shows text as the first menu line and in the tooltip.
func (t *Tray) SetStatus(text string) {
	log.Printf("[tapbridge] %s", text)
	if t == nil || t.mStatus == nil {
		return
	}
	t.mStatus.SetTitle(truncate(text, maxStatusLen))
	systray.SetTooltip("TapBridge — " + text)
}

// SetLastScanned shows the most recently scanned ID.
func (t *Tray) SetLastScanned(id string) {
	if t == nil || t.mLastID == nil {
		return
	}
	t.mLastID.SetTitle("Last scanned: " + truncate(id, 60))
}

// SetReaders lists the connected readers and checks the active one.
func (t *Tray) SetReaders(readers []string, active string) {
	if t == nil {
		return
	}
	t.slotMu.Lock()
	defer t.slotMu.Unlock()
	for i, slot := range t.slots {
		if slot == nil {
			continue
		}
		if i >= len(readers) {
			t.slotNames[i] = ""
			slot.Uncheck()
			slot.Hide()
			continue
		}
		t.slotNames[i] = readers[i]
		slot.SetTitle(readers[i])
		slot.Show()
		if readers[i] == active {
			slot.Check()
		} else {
			slot.Uncheck()
		}
	}
}

func (t *Tray) watchSlot(i int, slot *systray.MenuItem) {
	for range slot.ClickedCh {
		t.slotMu.Lock()
		name := t.slotNames[i]
		t.slotMu.Unlock()
		if name != "" && t.opts.OnSelectReader != nil {
			t.opts.OnSelectReader(name)
		}
	}
}

func (t *Tray) watchAutoStart(item *systray.MenuItem) {
	for range item.ClickedCh {
		if t.opts.OnAutoStart == nil {
			continue
		}
		enable := !item.Checked()
		if err := t.opts.OnAutoStart(enable); err != nil {
			t.SetStatus("Couldn't change Start at Login: " + err.Error())
			continue
		}
		if enable {
			item.Check()
			t.SetStatus("Start at Login enabled")
		} else {
			item.Uncheck()
			t.SetStatus("Start at Login disabled")
		}
	}
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
