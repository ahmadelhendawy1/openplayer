package main

import (
	"sync"
	"time"
)

// clickSorter tells single clicks from double clicks on the video picture.
// A single click runs onSingle after the double-click window has passed; a
// second click inside the window cancels it and runs onDouble instead.
type clickSorter struct {
	mu       sync.Mutex
	timer    *time.Timer
	first    time.Time
	window   func() time.Duration
	onSingle func()
	onDouble func()
}

func (c *clickSorter) click() {
	win := c.window()
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if c.timer != nil && now.Sub(c.first) <= win {
		c.timer.Stop()
		c.timer = nil
		go c.onDouble()
		return
	}
	c.first = now
	var t *time.Timer
	t = time.AfterFunc(win, func() {
		c.mu.Lock()
		fire := c.timer == t
		if fire {
			c.timer = nil
		}
		c.mu.Unlock()
		if fire {
			c.onSingle()
		}
	})
	c.timer = t
}
