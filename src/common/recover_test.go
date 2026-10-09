package common

import (
	"errors"
	"net"
	"syscall"
	"testing"
	"time"
)

func panickyErr() (err error) {
	defer RecoverErr("panickyErr", &err)
	panic("boom")
}

func TestRecoverErr_AssignsPOSIXError(t *testing.T) {
	err := panickyErr()
	if err == nil {
		t.Fatal("expected a recovered error")
	}
	if !errors.Is(err, syscall.EIO) {
		t.Fatalf("expected error wrapping syscall.EIO, got %v", err)
	}
}

func TestRecoverErr_NoPanicLeavesNil(t *testing.T) {
	func() (err error) {
		defer RecoverErr("noop", &err)
		return nil
	}()
	// A no-panic run must not fabricate an error.
	if panickyErrNoPanic() != nil {
		t.Fatal("expected nil error when no panic occurs")
	}
}

func panickyErrNoPanic() (err error) {
	defer RecoverErr("panickyErrNoPanic", &err)
	return nil
}

func TestRecoverErr_NilErrorPointer(t *testing.T) {
	// Must not itself panic when given a nil *error.
	func() {
		defer RecoverErr("nil-ptr", nil)
		panic("boom")
	}()
}

func TestRecoverErrClose_ClosesOnPanic(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c2.Close()

	err := func() (err error) {
		defer RecoverErrClose("close-on-panic", &err, c1)
		panic("boom")
	}()
	if !errors.Is(err, syscall.EIO) {
		t.Fatalf("expected syscall.EIO, got %v", err)
	}

	c2.SetReadDeadline(time.Now().Add(time.Second))
	if _, rerr := c2.Read(make([]byte, 1)); rerr == nil {
		t.Fatal("expected the closer to be closed after a recovered panic")
	}
}

func TestRecoverErrClose_NoPanicDoesNotClose(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c2.Close()
	defer c1.Close()

	func() (err error) {
		defer RecoverErrClose("no-panic", &err, c1)
		return nil
	}()

	// c1 is still open: a peer write with a reader available succeeds.
	go func() { _, _ = c1.Read(make([]byte, 1)) }()
	c2.SetWriteDeadline(time.Now().Add(time.Second))
	if _, err := c2.Write([]byte("x")); err != nil {
		t.Fatalf("expected c1 to remain open without a panic: %v", err)
	}
}

func TestRecoverClose_ClosesOnPanic(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c2.Close()

	func() {
		defer RecoverClose("goroutine-handler", c1)
		panic("boom")
	}()

	c2.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := c2.Read(make([]byte, 1)); err == nil {
		t.Fatal("expected the closer to be closed after a recovered panic")
	}
}

func TestRecoverClose_NoPanicDoesNotClose(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c2.Close()
	defer c1.Close()

	func() {
		defer RecoverClose("no-panic", c1)
	}()

	go func() { _, _ = c1.Read(make([]byte, 1)) }()
	c2.SetWriteDeadline(time.Now().Add(time.Second))
	if _, err := c2.Write([]byte("x")); err != nil {
		t.Fatalf("expected c1 to remain open without a panic: %v", err)
	}
}

func TestRecoverErrWith_CustomErrno(t *testing.T) {
	run := func() (err error) {
		defer RecoverErrWith("withEINVAL", syscall.EINVAL, &err)
		panic("boom")
	}
	if err := run(); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("expected syscall.EINVAL, got %v", err)
	}
}
