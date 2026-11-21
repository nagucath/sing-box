package reqtransport

import (
	"context"
	"crypto/tls"
	"errors"
	"net"

	infradial "github.com/wignerStan/infra/go/network/dial"
)

// DialTLS opens a caller-owned TLS stream using the same uTLS browser profile
// and certificate policy as RoundTrip. It is useful for HTTP protocol upgrades
// that a server acknowledges with 200 rather than a standard 101 response.
// The caller must honor the returned ALPN protocol and close the stream.
// Explicit HTTP proxies are supported by RoundTrip, not by this raw-stream API.
func (t *Transport) DialTLS(ctx context.Context, address string) (net.Conn, *tls.ConnectionState, error) {
	if t == nil || t.backend == nil || ctx == nil {
		return nil, nil, errors.New("TLS dial requires a transport and context")
	}
	if t.backend.Proxy != nil {
		return nil, nil, errors.New("raw TLS streams do not support a configured proxy")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || port == "" {
		return nil, nil, errors.New("TLS dial requires host:port")
	}
	plain, err := infradial.Connect(ctx, t.dialer, "tcp", address)
	if err != nil {
		return nil, nil, err
	}
	conn, state, err := t.backend.TLSHandshakeContext(ctx, address, plain)
	if err != nil {
		plain.Close()
		return nil, nil, err
	}
	if err = ctx.Err(); err != nil {
		conn.Close()
		return nil, nil, err
	}
	return conn, state, nil
}
