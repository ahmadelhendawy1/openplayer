package main

import (
	"sync/atomic"
	"testing"
	"time"
)

func newTestSorter() (*clickSorter, *int32, *int32) {
	var s, d int32
	return &clickSorter{
		window:   func() time.Duration { return 80 * time.Millisecond },
		onSingle: func() { atomic.AddInt32(&s, 1) },
		onDouble: func() { atomic.AddInt32(&d, 1) },
	}, &s, &d
}

func TestSingleClickPauses(t *testing.T) {
	c, s, d := newTestSorter()
	c.click()
	if atomic.LoadInt32(s) != 1 || atomic.LoadInt32(d) != 0 {
		t.Fatalf("single=%d double=%d", atomic.LoadInt32(s), atomic.LoadInt32(d))
	}
}

func TestDoubleClick(t *testing.T) {
	c, s, d := newTestSorter()
	c.click() // pauses at once
	if atomic.LoadInt32(s) != 1 {
		t.Fatal("single click must act immediately")
	}
	time.Sleep(30 * time.Millisecond)
	c.click() // double: undo + full screen
	time.Sleep(150 * time.Millisecond)
	if atomic.LoadInt32(s) != 1 || atomic.LoadInt32(d) != 1 {
		t.Fatalf("single=%d double=%d", atomic.LoadInt32(s), atomic.LoadInt32(d))
	}
}

func TestTwoSlowClicksAreTwoSingles(t *testing.T) {
	c, s, d := newTestSorter()
	c.click()
	time.Sleep(150 * time.Millisecond)
	c.click()
	time.Sleep(150 * time.Millisecond)
	if atomic.LoadInt32(s) != 2 || atomic.LoadInt32(d) != 0 {
		t.Fatalf("single=%d double=%d", atomic.LoadInt32(s), atomic.LoadInt32(d))
	}
}

func TestDoubleThenSingle(t *testing.T) {
	c, s, d := newTestSorter()
	c.click()
	c.click() // double: full screen
	time.Sleep(150 * time.Millisecond)
	c.click() // later single: pause
	time.Sleep(150 * time.Millisecond)
	if atomic.LoadInt32(s) != 2 || atomic.LoadInt32(d) != 1 {
		t.Fatalf("single=%d double=%d", atomic.LoadInt32(s), atomic.LoadInt32(d))
	}
}
