//go:build windows

package main

import (
	"errors"
	"fmt"
	"math"
	"syscall"
	"unsafe"
)

// Minimal libmpv binding that loads libmpv-2.dll at run time (no cgo, so the
// player cross-compiles from any OS and the engine DLL stays swappable).
type mpvLib struct {
	dll                                          *syscall.DLL
	create, setOptionString, initialize, command *syscall.Proc
	commandAsync, observeProperty                *syscall.Proc
	terminateDestroy, waitEvent, errorString     *syscall.Proc
}

// libmpv event ids and formats used here.
const (
	evShutdown       = 1
	evClientMessage  = 16
	evPropertyChange = 22

	fmtString = 1
	fmtFlag   = 3
)

type mpvHandle uintptr

type mpvEvent struct {
	ID   int32
	Data uintptr // event-specific payload pointer
}

func loadMPV(path string) (*mpvLib, error) {
	d, err := syscall.LoadDLL(path)
	if err != nil {
		return nil, fmt.Errorf("cannot load %s: %w", path, err)
	}
	l := &mpvLib{dll: d}
	for name, dst := range map[string]**syscall.Proc{
		"mpv_create": &l.create, "mpv_set_option_string": &l.setOptionString,
		"mpv_initialize": &l.initialize, "mpv_command": &l.command,
		"mpv_command_async": &l.commandAsync, "mpv_observe_property": &l.observeProperty,
		"mpv_terminate_destroy": &l.terminateDestroy, "mpv_wait_event": &l.waitEvent,
		"mpv_error_string": &l.errorString,
	} {
		if *dst, err = d.FindProc(name); err != nil {
			return nil, fmt.Errorf("%s is not a libmpv build (%w)", path, err)
		}
	}
	return l, nil
}

func (l *mpvLib) err(code uintptr) error {
	if int32(code) >= 0 {
		return nil
	}
	r, _, _ := l.errorString.Call(code)
	return errors.New(cstr(r))
}

func (l *mpvLib) New() (mpvHandle, error) {
	h, _, _ := l.create.Call()
	if h == 0 {
		return 0, errors.New("mpv_create failed")
	}
	return mpvHandle(h), nil
}

func (l *mpvLib) Option(h mpvHandle, name, value string) error {
	n, _ := syscall.BytePtrFromString(name)
	v, _ := syscall.BytePtrFromString(value)
	r, _, _ := l.setOptionString.Call(uintptr(h), uintptr(unsafe.Pointer(n)), uintptr(unsafe.Pointer(v)))
	if e := l.err(r); e != nil {
		return fmt.Errorf("option %s=%s: %w", name, value, e)
	}
	return nil
}

func (l *mpvLib) Init(h mpvHandle) error {
	r, _, _ := l.initialize.Call(uintptr(h))
	return l.err(r)
}

func argv(args []string) ([]*byte, error) {
	arr := make([]*byte, len(args)+1) // NULL-terminated char*[]
	for i, a := range args {
		p, err := syscall.BytePtrFromString(a)
		if err != nil {
			return nil, err
		}
		arr[i] = p
	}
	return arr, nil
}

// Command runs a command and waits. Do NOT call it from the window thread
// once the window exists: mpv may be waiting on that thread. Use Async.
func (l *mpvLib) Command(h mpvHandle, args ...string) error {
	arr, err := argv(args)
	if err != nil {
		return err
	}
	r, _, _ := l.command.Call(uintptr(h), uintptr(unsafe.Pointer(&arr[0])))
	if e := l.err(r); e != nil {
		return fmt.Errorf("command %v: %w", args, e)
	}
	return nil
}

// Async queues a command and returns immediately (safe from any thread).
func (l *mpvLib) Async(h mpvHandle, args ...string) {
	arr, err := argv(args)
	if err != nil {
		return
	}
	// mpv copies the argument strings before returning.
	l.commandAsync.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&arr[0])))
}

// Observe asks mpv to send a property-change event for name.
func (l *mpvLib) Observe(h mpvHandle, name string, format int) error {
	n, _ := syscall.BytePtrFromString(name)
	r, _, _ := l.observeProperty.Call(uintptr(h), 0, uintptr(unsafe.Pointer(n)), uintptr(format))
	return l.err(r)
}

// Wait returns the next event (ID 0 = timeout). The double argument is
// passed as raw bits: Go's Windows syscall stub copies the first four
// arguments into both integer and XMM registers.
func (l *mpvLib) Wait(h mpvHandle, seconds float64) mpvEvent {
	r, _, _ := l.waitEvent.Call(uintptr(h), uintptr(math.Float64bits(seconds)))
	if r == 0 {
		return mpvEvent{}
	}
	ev := ptrAt(r) // mpv_event: int event_id; int error; uint64 userdata; void *data
	return mpvEvent{ID: *(*int32)(ev), Data: *(*uintptr)(unsafe.Add(ev, 16))}
}

func (l *mpvLib) Destroy(h mpvHandle) { l.terminateDestroy.Call(uintptr(h)) }

// propertyChange decodes an evPropertyChange payload (mpv_event_property).
func propertyChange(data uintptr) (name string, format int32, value uintptr) {
	if data == 0 {
		return
	}
	p := ptrAt(data)
	name = cstr(*(*uintptr)(p))
	format = *(*int32)(unsafe.Add(p, 8))
	value = *(*uintptr)(unsafe.Add(p, 16))
	return
}

func propFlag(value uintptr) (bool, bool) {
	if value == 0 {
		return false, false
	}
	return *(*int32)(ptrAt(value)) != 0, true
}

func propString(value uintptr) string {
	if value == 0 {
		return ""
	}
	return cstr(*(*uintptr)(ptrAt(value)))
}

// clientMessage returns the arguments of a script-message event.
func clientMessage(data uintptr) []string {
	if data == 0 {
		return nil
	}
	p := ptrAt(data) // mpv_event_client_message: int num_args; const char **args
	n := int(*(*int32)(p))
	if n < 1 || n > 16 {
		return nil
	}
	args := ptrAt(*(*uintptr)(unsafe.Add(p, 8)))
	out := make([]string, n)
	for i := range out {
		out[i] = cstr(*(*uintptr)(unsafe.Add(args, i*8)))
	}
	return out
}
