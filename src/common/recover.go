// Package common provides shared primitives for momo: configuration, hashing,
// checksums, CRUSH placement, and the unified panic-recovery helpers (Rules 37
// and 43) used across the transport and storage layers.
package common

import (
	"fmt"
	"io"
	"log"
	"syscall"
)

// RecoverErr implements the Rule 37 (Unified Observable Panic Recovery) pattern
// for functions with a named error return: it logs the recovered panic to Stderr
// and assigns an error wrapping syscall.EIO to *err so the failure is observable
// by the caller instead of being silently swallowed.
//
// Use it directly as a deferred function so that recover() takes effect:
//
//	func Do() (err error) {
//		defer common.RecoverErr("Do", &err)
//		// ...
//	}
func RecoverErr(op string, err *error) {
	if r := recover(); r != nil {
		log.Printf("CRITICAL: Panic recovered in %s: %v", op, r)
		if err != nil {
			*err = fmt.Errorf("panic in %s: %v: %w", op, r, syscall.EIO)
		}
	}
}

// RecoverErrWith is RecoverErr with an explicit POSIX error number. Use it when
// a handler maps panics to a constant other than syscall.EIO (e.g. syscall.EINVAL
// for a validation loop). It is zero-allocation: op and errno are passed by value.
//
// Like RecoverErr it calls recover() directly, so it MUST be used directly as a
// deferred function — wrapping it in another closure would make recover() return
// nil.
func RecoverErrWith(op string, errno syscall.Errno, err *error) {
	if r := recover(); r != nil {
		log.Printf("CRITICAL: Panic recovered in %s: %v", op, r)
		if err != nil {
			*err = fmt.Errorf("panic in %s: %v: %w", op, r, errno)
		}
	}
}

// RecoverErrClose combines Rule 37 (log + POSIX-mapped named return) with
// Rule 43 (Panic-Safe Resource Releasing): after a recovered panic it also
// closes the supplied resource when non-nil, so a panicking connection-wrapping
// operation cannot leave a zombie socket/file open.
//
// Use it directly as a deferred function for recover() to take effect.
func RecoverErrClose(op string, err *error, closer io.Closer) {
	if r := recover(); r != nil {
		log.Printf("CRITICAL: Panic recovered in %s: %v", op, r)
		if err != nil {
			*err = fmt.Errorf("panic in %s: %v: %w", op, r, syscall.EIO)
		}
		if closer != nil {
			_ = closer.Close()
		}
	}
}

// RecoverClose implements Rule 43 (Panic-Safe Resource Releasing) for
// panic-recovery blocks that have no named error return — typically goroutine
// closures that hold a connection or file. It logs the panic and closes the
// resource. Use it directly as a deferred function.
func RecoverClose(op string, closer io.Closer) {
	if r := recover(); r != nil {
		log.Printf("CRITICAL: Panic recovered in %s: %v", op, r)
		if closer != nil {
			_ = closer.Close()
		}
	}
}
