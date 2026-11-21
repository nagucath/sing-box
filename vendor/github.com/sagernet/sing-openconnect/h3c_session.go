package openconnect

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sagernet/sing/common/buf"
	E "github.com/sagernet/sing/common/exceptions"
)

type h3cSession struct {
	readDoneOnce  sync.Once
	starting      bool
	client        *Client
	state         *h3cObtainedSession
	ctx           context.Context
	cancel        context.CancelFunc
	mu, write     sync.Mutex
	conn          net.Conn
	configuration TunnelConfiguration
	ready         atomic.Bool
	closed        bool
	done          chan error
	readDone      chan struct{}
	stopOnce      sync.Once
}

func (f *h3cFrontend) ConnectTunnel(ctx context.Context, obtained obtainedSession) (clientSession, error) {
	state, ok := obtained.(*h3cObtainedSession)
	if !ok || state == nil || state.frontend != f {
		return nil, E.Extend(ErrProtocolNotSupported, "invalid H3C obtained session")
	}
	ctx, cancel := context.WithCancel(ctx)
	return &h3cSession{client: f.client, state: state, ctx: ctx, cancel: cancel, done: make(chan error, 1), readDone: make(chan struct{})}, nil
}
func (s *h3cSession) Start() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		s.endRead()
		return ErrClientClosed
	}
	s.starting = true
	s.mu.Unlock()
	bounded, cancel := context.WithTimeout(s.ctx, 20*time.Second)
	defer cancel()
	address := s.client.serverURL.Host
	if s.client.serverURL.Port() == "" {
		address = net.JoinHostPort(s.client.serverURL.Hostname(), "443")
	}
	conn, tlsState, err := s.state.frontend.dialTLS(bounded, address)
	if err != nil {
		s.endRead()
		return err
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		conn.Close()
		s.endRead()
		return ErrClientClosed
	}
	s.conn = conn
	s.mu.Unlock()
	fail := func(err error) error { conn.Close(); s.endRead(); return err }
	if tlsState.NegotiatedProtocol != "" && tlsState.NegotiatedProtocol != "http/1.1" {
		return fail(E.Extend(ErrProtocolNotSupported, "H3C requires HTTP/1.1"))
	}
	if err = conn.SetDeadline(time.Now().Add(20 * time.Second)); err != nil {
		return fail(err)
	}
	r := &http.Request{Method: "NET_EXTEND", URL: s.client.serverURL, Host: s.client.serverURL.Host, Header: make(http.Header)}
	r.URL = cloneH3CURL(s.client.serverURL)
	r.URL.Path = "/"
	r.URL.RawPath = ""
	r.URL.RawQuery = ""
	agent := s.client.options.UserAgent
	if agent == "" {
		agent = h3cUserAgent
	}
	r.Header.Set("User-Agent", agent)
	for _, cookie := range s.state.jar.Cookies(s.client.serverURL) {
		r.AddCookie(cookie)
	}
	if r.Header.Get("Cookie") == "" {
		return fail(markTerminal(ErrAuthenticationFailed))
	}
	if err = r.Write(conn); err != nil {
		return fail(err)
	}
	reader := bufio.NewReaderSize(conn, 8192)
	response, err := readH3CUpgrade(reader)
	if err != nil {
		return fail(err)
	}
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return fail(markTerminal(ErrAuthenticationFailed))
	}
	if response.StatusCode != 200 || response.ContentLength > 0 || len(response.TransferEncoding) > 0 {
		return fail(E.Extend(ErrProtocolNotSupported, "H3C NET_EXTEND rejected or has an unexpected body"))
	}
	configuration, err := parseH3CConfiguration(response.Header, s.client.options.MTU)
	if err != nil {
		return fail(markTerminal(err))
	}
	if err = conn.SetDeadline(time.Time{}); err != nil {
		return fail(err)
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fail(ErrClientClosed)
	}
	s.configuration = configuration
	s.ready.Store(true)
	s.mu.Unlock()
	s.client.setActiveTransport(s, TransportTLS)
	go s.receive(reader)
	go func() {
		select {
		case <-s.ctx.Done():
			s.Fail(s.ctx.Err())
		case <-s.readDone:
		}
	}()
	return nil
}
func cloneH3CURL(u *url.URL) *url.URL { v := *u; return &v }

func readH3CUpgrade(reader *bufio.Reader) (*http.Response, error) {
	var data []byte
	for {
		line, err := reader.ReadSlice('\n')
		if err != nil {
			return nil, E.Extend(ErrProtocolNotSupported, "H3C upgrade header read failed")
		}
		if len(data)+len(line) > 65536 {
			return nil, E.Extend(ErrProtocolNotSupported, "H3C upgrade headers exceed 64 KiB")
		}
		data = append(data, line...)
		if bytes.Equal(line, []byte("\r\n")) {
			break
		}
	}
	return http.ReadResponse(bufio.NewReader(bytes.NewReader(data)), nil)
}
func parseH3CConfiguration(headers http.Header, mtu uint32) (TunnelConfiguration, error) {
	ip, err := netip.ParseAddr(headers.Get("IPADDRESS"))
	if err != nil || !ip.Is4() || ip.IsUnspecified() {
		return TunnelConfiguration{}, E.Extend(errTunnelConfiguration, "H3C assigned IPv4 is invalid")
	}
	bits, err := strconv.Atoi(headers.Get("SUBNETMASK"))
	if err != nil || bits < 0 || bits > 32 {
		return TunnelConfiguration{}, E.Extend(errTunnelConfiguration, "H3C subnet mask is invalid")
	}
	if mtu == 0 {
		mtu = 1400
	}
	if mtu > 65535 {
		return TunnelConfiguration{}, E.Extend(errTunnelConfiguration, "H3C MTU exceeds frame limit")
	}
	configuration := TunnelConfiguration{MTU: mtu, Addresses: []netip.Prefix{netip.PrefixFrom(ip, bits)}, defaultIPv4RouteSuppressed: true, defaultIPv6RouteSuppressed: true}
	for _, raw := range strings.Split(headers.Get("ROUTES"), ";") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		prefix, e := netip.ParsePrefix(raw)
		if e != nil || !prefix.Addr().Is4() {
			return TunnelConfiguration{}, E.Extend(errTunnelConfiguration, "invalid H3C IPv4 route")
		}
		configuration.Routes = append(configuration.Routes, TunnelRoute{Prefix: prefix.Masked()})
	}
	return configuration, nil
}
func (s *h3cSession) receive(reader io.Reader) {
	defer s.endRead()
	for {
		var header [4]byte
		if _, err := io.ReadFull(reader, header[:]); err != nil {
			s.Fail(err)
			return
		}
		size := int(binary.BigEndian.Uint16(header[2:]))
		if header[0] != 1 || header[1] != 0 {
			s.Fail(E.Extend(ErrProtocolNotSupported, "invalid H3C frame"))
			return
		}
		payload := make([]byte, size)
		if _, err := io.ReadFull(reader, payload); err != nil {
			s.Fail(err)
			return
		}
		if len(payload) == 0 || payload[0] == 2 {
			continue
		}
		if len(payload) < 20 || payload[0]>>4 != 4 || int(binary.BigEndian.Uint16(payload[2:4])) != len(payload) {
			s.Fail(E.Extend(ErrProtocolNotSupported, "invalid H3C IPv4 packet"))
			return
		}
		s.client.pushIncomingDataPacketContext(s.ctx, buf.As(payload))
	}
}
func (s *h3cSession) WriteDataPackets(packets [][]byte) error {
	return s.WriteDataPacketBuffers(newPacketBuffersFrom(packets))
}
func (s *h3cSession) WriteDataPacketBuffers(packets []*buf.Buffer) error {
	defer buf.ReleaseMulti(packets)
	s.write.Lock()
	defer s.write.Unlock()
	if !s.ready.Load() {
		return ErrDataChannelNotReady
	}
	for _, packet := range packets {
		payload := packet.Bytes()
		if len(payload) < 20 || len(payload) > int(s.configuration.MTU) || payload[0]>>4 != 4 || int(binary.BigEndian.Uint16(payload[2:4])) != len(payload) {
			return E.Extend(ErrProtocolNotSupported, "invalid outbound H3C IPv4 packet")
		}
		frame := make([]byte, 4+len(payload))
		frame[0] = 1
		binary.BigEndian.PutUint16(frame[2:], uint16(len(payload)))
		copy(frame[4:], payload)
		for len(frame) > 0 {
			n, err := s.conn.Write(frame)
			if err != nil {
				s.Fail(err)
				return err
			}
			if n == 0 {
				return io.ErrShortWrite
			}
			frame = frame[n:]
		}
	}
	return nil
}
func (s *h3cSession) Fail(err error) {
	s.stopOnce.Do(func() {
		s.ready.Store(false)
		s.cancel()
		s.mu.Lock()
		s.closed = true
		conn := s.conn
		s.mu.Unlock()
		if conn != nil {
			conn.Close()
		}
		s.done <- err
	})
}
func (s *h3cSession) endRead() { s.readDoneOnce.Do(func() { close(s.readDone) }) }
func (s *h3cSession) Close() error {
	s.mu.Lock()
	starting := s.starting
	s.mu.Unlock()
	if !starting {
		s.endRead()
	}
	s.Fail(ErrClientClosed)
	<-s.readDone
	s.client.stopActiveTransport(s)
	return nil
}
func (s *h3cSession) Done() <-chan error { return s.done }
func (s *h3cSession) Ready() bool        { return s.ready.Load() }
func (s *h3cSession) TunnelConfiguration() TunnelConfiguration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneTunnelConfiguration(s.configuration)
}
