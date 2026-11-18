//go:build windows

package main

// Left mouse button on the video picture:
//   one click    -> pause / play
//   double click -> enter / leave full screen (and does NOT pause)
// mpv sends "openplayer-click" for every left press on the bare video (uosc
// keeps the clicks that land on its own buttons and bars). The single-click
// action waits for the double-click time, so a double click never pauses and
// resumes the video on its way to full screen.

import "time"

var (
	pGetDoubleClickTime = user32.NewProc("GetDoubleClickTime")

	videoClicks = &clickSorter{
		// follows the Windows mouse setting, kept between 200 and 400 ms so a
		// single click still feels instant
		window: func() time.Duration {
			ms, _, _ := pGetDoubleClickTime.Call()
			return time.Duration(min(max(int(ms), 200), 400)) * time.Millisecond
		},
		onSingle: func() {
			if mp != 0 {
				mpv.Async(mp, "cycle", "pause")
			}
		},
		onDouble: func() { pPostMessageW.Call(mainWnd, wmCommand, idmFullscreen, 0) },
	}
)

// videoClick runs on the mpv event goroutine.
func videoClick() { videoClicks.click() }
