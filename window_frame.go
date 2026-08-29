package main

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"devkit/internal/uistate"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// uistate's own package doc names "a window bound" as one of the things it
// exists to keep across restarts. This is that bound: reopening at a fixed
// 1200x760 in the middle of the primary display is the single most un-native
// thing a desktop app can do.
const windowFrameKey = "window.frame"

// Debounce for size/move writes. WindowDidResize and WindowDidMove fire on
// every pixel of a drag; without this each drag would be hundreds of SQLite
// writes. Long enough to collapse a drag, short enough that a force-quit
// straight after a resize still keeps it.
const windowFrameSaveDelay = 700 * time.Millisecond

// Tolerance for treating a frame as unchanged. Bounds() reports DIP derived
// from physical pixels through the display's scale factor, and that division
// does not always round back to the value that was restored — on a 2x display
// a restored 1100x740 reads back as 1099x739. Writing that back would shave a
// pixel off the window on every single restart, so a frame within this many
// DIP of the stored one is treated as the same frame and not saved.
const windowFrameEpsilon = 2

type windowFrame struct {
	X         int  `json:"x"`
	Y         int  `json:"y"`
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	Maximised bool `json:"maximised"`
}

func (f windowFrame) sameAs(other windowFrame) bool {
	within := func(a, b int) bool { return max(a-b, b-a) <= windowFrameEpsilon }
	return f.Maximised == other.Maximised &&
		within(f.X, other.X) && within(f.Y, other.Y) &&
		within(f.Width, other.Width) && within(f.Height, other.Height)
}

// loadWindowFrame returns the persisted frame, or false when there is none, it
// is unreadable, or it is smaller than the window's own floor — a frame that
// violates MinWidth/MinHeight would be silently resized by the platform on
// restore and then saved back wrong.
func loadWindowFrame(store *uistate.Service) (windowFrame, bool) {
	if store == nil {
		return windowFrame{}, false
	}
	raw, found, err := store.Get(windowFrameKey)
	if err != nil {
		log.Printf("read window frame: %v", err)
		return windowFrame{}, false
	}
	if !found {
		return windowFrame{}, false
	}
	var frame windowFrame
	if err := json.Unmarshal([]byte(raw), &frame); err != nil {
		return windowFrame{}, false
	}
	if frame.Width < minWindowWidth || frame.Height < minWindowHeight {
		return windowFrame{}, false
	}
	return frame, true
}

// trackWindowFrame persists the window's geometry as the user changes it.
func trackWindowFrame(win application.Window, store *uistate.Service) {
	if win == nil || store == nil {
		return
	}
	var (
		mu    sync.Mutex
		timer *time.Timer
	)

	save := func() {
		bounds := win.Bounds()
		if bounds.Width < minWindowWidth || bounds.Height < minWindowHeight {
			// A zero Rect is what Bounds() returns for a destroyed window.
			return
		}
		frame := windowFrame{
			X:         bounds.X,
			Y:         bounds.Y,
			Width:     bounds.Width,
			Height:    bounds.Height,
			Maximised: win.IsMaximised(),
		}
		previous, hadPrevious := loadWindowFrame(store)
		if frame.Maximised && hadPrevious {
			// A maximised window reports the maximised rect. Storing it would
			// lose the size to restore *down* to, so keep the last normal
			// frame and only record that it was left maximised.
			frame.X, frame.Y = previous.X, previous.Y
			frame.Width, frame.Height = previous.Width, previous.Height
		}
		if hadPrevious && frame.sameAs(previous) {
			return
		}
		encoded, err := json.Marshal(frame)
		if err != nil {
			return
		}
		if err := store.Set(windowFrameKey, string(encoded)); err != nil {
			log.Printf("persist window frame: %v", err)
		}
	}

	schedule := func(*application.WindowEvent) {
		mu.Lock()
		defer mu.Unlock()
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(windowFrameSaveDelay, save)
	}
	win.OnWindowEvent(events.Common.WindowDidResize, schedule)
	win.OnWindowEvent(events.Common.WindowDidMove, schedule)
	win.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		mu.Lock()
		if timer != nil {
			timer.Stop()
		}
		mu.Unlock()
		save()
	})
}

// recentreIfOffScreen moves the window back onto a visible display when the one
// it was last saved on is gone — undock a laptop, relaunch, and a restored
// frame can otherwise land entirely outside every attached screen, which looks
// exactly like the app failing to start. Only a window with no overlap at all
// is moved; a partially off-screen window is the user's own arrangement.
func recentreIfOffScreen(win application.Window, screens *application.ScreenManager) {
	if win == nil || screens == nil {
		return
	}
	bounds := win.Bounds()
	if bounds.Width == 0 || bounds.Height == 0 {
		return
	}
	all := screens.GetAll()
	if len(all) == 0 {
		return
	}
	for _, screen := range all {
		if screen == nil {
			continue
		}
		if !bounds.Intersect(screen.WorkArea).IsEmpty() {
			return
		}
	}
	primary := screens.GetPrimary()
	if primary == nil {
		primary = all[0]
	}
	work := primary.WorkArea
	width := min(bounds.Width, work.Width)
	height := min(bounds.Height, work.Height)
	win.SetBounds(application.Rect{
		X:      work.X + (work.Width-width)/2,
		Y:      work.Y + (work.Height-height)/2,
		Width:  width,
		Height: height,
	})
}
