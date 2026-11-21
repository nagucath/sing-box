// Package dial hardens native connection establishment and context ownership.
package dial

import (
	"context"
	"errors"
	"net"
	"time"
)

type Dialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}
type originKey struct{}

// Track carries the request lifetime through native transports that detach dial contexts.
func Track(ctx context.Context) context.Context { return context.WithValue(ctx, originKey{}, ctx) }
func Context(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	original, _ := ctx.Value(originKey{}).(context.Context)
	deadline := time.Now().Add(timeout)
	if original != nil {
		if value, ok := original.Deadline(); ok && value.Before(deadline) {
			deadline = value
		}
	}
	bounded, cancel := context.WithDeadline(ctx, deadline)
	var stop func() bool
	if original != nil {
		stop = context.AfterFunc(original, cancel)
		if original.Err() != nil {
			cancel()
		}
	}
	return bounded, func() {
		if stop != nil {
			stop()
		}
		cancel()
	}
}

// Connect delegates dialing and retains ownership until native success.
// Error and cancellation paths close even an unexpectedly non-nil connection.
func Connect(ctx context.Context, native Dialer, network, address string) (net.Conn, error) {
	if ctx == nil || native == nil {
		return nil, errors.New("dial requires context and native dialer")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	conn, err := native.DialContext(ctx, network, address)
	if canceled := ctx.Err(); canceled != nil {
		if conn != nil {
			conn.Close()
		}
		return nil, canceled
	}
	if err != nil {
		if conn != nil {
			_ = conn.Close()
		}
		return nil, err
	}
	if conn == nil {
		return nil, errors.New("native dial returned no connection")
	}
	return conn, nil
}
