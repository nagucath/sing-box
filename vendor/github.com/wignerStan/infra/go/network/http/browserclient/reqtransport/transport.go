// Package reqtransport provides in-process browser-profile HTTP exchanges.
// It binds imroc/req's uTLS and HTTP/2 profiles without a browser worker.
package reqtransport

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/imroc/req/v3"
	infradial "github.com/wignerStan/infra/go/network/dial"
)

// Options selects a bundled browser profile and explicit connection settings.
// Profiles are chrome (120), firefox (120), and safari (16.6), as bundled by
// the pinned req release. They are not claims about the latest browser release.
// RootCAs is cloned. Certificate verification is enabled unless explicitly
// disabled. ProxyURL is explicit; an empty URL ignores proxy environment variables.
type Options struct {
	// Dialer optionally supplies the native connection owner (for proxy routing).
	Dialer infradial.Dialer
	// VerifyConnection runs on every completed TLS handshake before HTTP bytes.
	VerifyConnection   func(tls.ConnectionState) error
	Profile            string
	RootCAs            *x509.CertPool
	InsecureSkipVerify bool
	ProxyURL           string
}

// Transport implements http.RoundTripper. The caller owns http.Client policy,
// its CookieJar, redirects, deadlines, credentials, and response-body lifecycle.
// Use CloseIdleConnections when finished; active requests retain their contexts.
type Transport struct {
	dialer  infradial.Dialer
	backend *req.Transport
	headers http.Header
}

// New validates all supported settings before constructing a native transport.
// No network connection or subprocess is started during construction.
func New(options Options) (*Transport, error) {
	profile := options.Profile
	if profile == "" {
		profile = "chrome"
	}
	if profile != "chrome" && profile != "firefox" && profile != "safari" {
		return nil, errors.New("unsupported browser profile")
	}
	var proxy *url.URL
	if options.ProxyURL != "" {
		var err error
		proxy, err = url.Parse(options.ProxyURL)
		if err != nil || proxy.Hostname() == "" || proxy.Fragment != "" || proxy.RawQuery != "" || proxy.Path != "" {
			return nil, errors.New("invalid explicit proxy URL")
		}
		switch proxy.Scheme {
		case "http", "https", "socks5", "socks5h":
		default:
			return nil, errors.New("unsupported proxy scheme")
		}
	}
	dialer := options.Dialer
	if dialer == nil {
		dialer = &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	}
	backend := req.C().SetProxy(nil).SetDial(func(ctx context.Context, network, address string) (net.Conn, error) {
		return infradial.Connect(ctx, dialer, network, address)
	})
	switch profile {
	case "chrome":
		backend.ImpersonateChrome()
	case "firefox":
		backend.ImpersonateFirefox()
	case "safari":
		backend.ImpersonateSafari()
	}
	config := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: options.InsecureSkipVerify}
	if options.RootCAs != nil {
		config.RootCAs = options.RootCAs.Clone()
	}
	backend.SetTLSClientConfig(config)

	if options.VerifyConnection != nil {
		handshake := backend.GetTransport().TLSHandshakeContext
		backend.GetTransport().SetTLSHandshake(func(ctx context.Context, address string, plain net.Conn) (net.Conn, *tls.ConnectionState, error) {
			conn, state, err := handshake(ctx, address, plain)
			if err != nil {
				return nil, nil, err
			}
			if err = options.VerifyConnection(*state); err != nil {
				conn.Close()
				return nil, nil, err
			}
			return conn, state, nil
		})
	}

	if proxy != nil {
		backend.SetProxy(http.ProxyURL(proxy))
	}
	return &Transport{dialer: dialer, backend: backend.GetTransport(), headers: backend.Headers.Clone()}, nil
}

// RoundTrip retains the browser profile while allowing explicit per-request
// headers to override profile defaults. It never mutates the caller's headers.
// It does not buffer bodies, follow redirects, log credentials, or add retries.
func (t *Transport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil {
		return nil, errors.New("nil HTTP request")
	}
	if t == nil || t.backend == nil || request.URL == nil || request.URL.Host == "" || (request.URL.Scheme != "http" && request.URL.Scheme != "https") {
		if request.Body != nil {
			_ = request.Body.Close()
		}
		return nil, errors.New("native browser transport requires an absolute HTTP(S) request")
	}
	cloned := request.Clone(request.Context())
	if cloned.Header == nil {
		cloned.Header = make(http.Header)
	}
	for key, values := range t.headers {
		if _, set := cloned.Header[http.CanonicalHeaderKey(key)]; !set {
			cloned.Header[http.CanonicalHeaderKey(key)] = append([]string(nil), values...)
		}
	}
	return t.backend.RoundTrip(cloned)
}

// CloseIdleConnections releases reusable connections; it does not cancel calls.
func (t *Transport) CloseIdleConnections() {
	if t != nil && t.backend != nil {
		t.backend.CloseIdleConnections()
	}
}
