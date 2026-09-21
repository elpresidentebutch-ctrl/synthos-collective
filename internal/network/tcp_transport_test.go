package network

import (
	"net"
	"testing"
	"time"
)

// TestSlowLorisConnectionIsDroppedAfterReadTimeout guards against the exact
// bug the audit found in TCPTransport.handleConn: it used to have no read
// deadline at all, so a connection that sent a valid length prefix and then
// simply stopped sending -- the classic slow-loris pattern -- would tie up
// its goroutine, buffer, and file descriptor forever. This dials the
// server directly (bypassing the signed-envelope layer entirely, since the
// bug is at the raw-framing level below that), sends only a length prefix,
// and confirms the server actively closes the connection once ReadTimeout
// elapses rather than hanging indefinitely.
func TestSlowLorisConnectionIsDroppedAfterReadTimeout(t *testing.T) {
	tr := NewTCPTransport("hub", "127.0.0.1:0", nil)
	// Override the production default (tcpReadTimeout, 30s) with something
	// short so this test doesn't take 30 seconds to prove the point.
	tr.ReadTimeout = 150 * time.Millisecond
	if err := tr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tr.Close()

	addr := tr.listener.Addr().String()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	// A valid 4-byte big-endian length prefix claiming a 100-byte message
	// -- but the body is deliberately never sent.
	if _, err := conn.Write([]byte{0, 0, 0, 100}); err != nil {
		t.Fatalf("Write length prefix: %v", err)
	}

	// If the server is still hanging in io.ReadFull with no deadline (the
	// pre-fix bug), this Read blocks until our own deadline below fires,
	// which is generous enough to clearly distinguish "server closed it
	// promptly" from "server never closes it."
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}

	start := time.Now()
	buf := make([]byte, 1)
	_, readErr := conn.Read(buf)
	elapsed := time.Since(start)

	if readErr == nil {
		t.Fatalf("expected the server to close the stalled connection, got a successful read")
	}
	if ne, ok := readErr.(net.Error); ok && ne.Timeout() {
		t.Fatalf("server never closed the stalled connection within 3s (read timed out on our side instead) -- ReadTimeout is not being enforced")
	}
	// Some error other than our own timeout -- expected to be a connection
	// close (EOF or reset) from the server enforcing its ReadTimeout.
	if elapsed > 2*time.Second {
		t.Fatalf("server took %s to drop a stalled connection with a %s ReadTimeout -- too slow, ReadTimeout may not be applied per-message", elapsed, tr.ReadTimeout)
	}
}

// TestReadTimeoutDefaultsWhenUnset confirms readTimeout() falls back to the
// production default (tcpReadTimeout) for a TCPTransport that never had its
// ReadTimeout field set (e.g. NewTCPTransport, or a zero-value struct).
func TestReadTimeoutDefaultsWhenUnset(t *testing.T) {
	tr := NewTCPTransport("hub", "127.0.0.1:0", nil)
	if tr.readTimeout() != tcpReadTimeout {
		t.Fatalf("readTimeout() = %s, want the default %s", tr.readTimeout(), tcpReadTimeout)
	}
	tr.ReadTimeout = 5 * time.Second
	if tr.readTimeout() != 5*time.Second {
		t.Fatalf("readTimeout() = %s, want the overridden 5s", tr.readTimeout())
	}
}
