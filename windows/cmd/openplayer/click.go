package main

import (
	"sync"
	"time"
)

// clickSorter tells single clicks from double clicks on the video picture.
// onSingle runs at once on the first click, so pausing feels instant. A
// second click inside the double-click window runs onDouble, which undoes
// the first click (play/pause back) and toggles full screen.
type clickSorter struct {
	mu       sync.Mutex
	armed    bool // a first click is waiting for a possible second one
	first    time.Time
	window   func() time.Duration
	onSingle func()
	onDouble func()
}

func (c *clickSorter) click() {
	win := c.window()
	c.mu.Lock()
	now := time.Now()
	if c.armed && now.Sub(c.first) <= win {
		c.armed = false
		c.mu.Unlock()
		c.onDouble()
		return
	}
	c.first, c.armed = now, true
	c.mu.Unlock()
	c.onSingle()
}
