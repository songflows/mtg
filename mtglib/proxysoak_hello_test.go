package mtglib

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/9seconds/mtg/v2/essentials"
	"github.com/9seconds/mtg/v2/mtglib/internal/tls/fake"
)

type memConn struct{ buf *bytes.Buffer }

func (m *memConn) Read(p []byte) (int, error)         { return m.buf.Read(p) }
func (m *memConn) Write(p []byte) (int, error)        { return len(p), nil }
func (m *memConn) Close() error                       { return nil }
func (m *memConn) LocalAddr() net.Addr                { return nil }
func (m *memConn) RemoteAddr() net.Addr               { return nil }
func (m *memConn) SetDeadline(t time.Time) error      { return nil }
func (m *memConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *memConn) SetWriteDeadline(t time.Time) error { return nil }
func (m *memConn) CloseRead() error                   { return nil }
func (m *memConn) CloseWrite() error                  { return nil }

func netPipe(t *testing.T) (server, client net.Conn) {
	t.Helper()

	s, c := net.Pipe()

	return s, c
}

func buildProxysoakHello(t *testing.T, secret Secret, now time.Time) []byte {
	t.Helper()

	sessionID := make([]byte, 32)
	if _, err := rand.Read(sessionID); err != nil {
		t.Fatal(err)
	}

	const cipherSuite = 0x1301
	ext := buildProxysoakSNI(secret.Host)
	bodyLen := 2 + 32 + 1 + len(sessionID) + 2 + 2 + 1 + 1 + 2 + len(ext)
	recordLen := 1 + 3 + bodyLen

	var full []byte
	full = append(full, 0x16, 3, 1)
	full = appendUint16PS(full, uint16(recordLen))
	full = append(full, 0x01)
	full = appendUint24PS(full, uint32(bodyLen))
	full = appendUint16PS(full, 0x0303)
	full = append(full, make([]byte, 32)...)
	full = append(full, byte(len(sessionID)))
	full = append(full, sessionID...)
	full = appendUint16PS(full, 2)
	full = appendUint16PS(full, cipherSuite)
	full = append(full, 1, 0)
	full = appendUint16PS(full, uint16(len(ext)))
	full = append(full, ext...)

	const randomOffset = 11

	digest := hmac.New(sha256.New, secret.Key[:])
	digest.Write(full[:randomOffset])
	digest.Write(make([]byte, 32))
	digest.Write(full[randomOffset+32:])

	sum := digest.Sum(nil)
	ts := make([]byte, 4)
	binary.LittleEndian.PutUint32(ts, uint32(now.Unix()))

	for i := 0; i < 28; i++ {
		full[randomOffset+i] = sum[i]
	}

	for i := 0; i < 4; i++ {
		full[randomOffset+28+i] = sum[28+i] ^ ts[i]
	}

	return full
}

func buildProxysoakSNI(hostname string) []byte {
	host := []byte(hostname)
	var b []byte

	b = appendUint16PS(b, 0)
	listLen := 1 + 2 + len(host)
	b = appendUint16PS(b, uint16(listLen+2))
	b = appendUint16PS(b, uint16(listLen))
	b = append(b, 0)
	b = appendUint16PS(b, uint16(len(host)))
	b = append(b, host...)

	return b
}

func appendUint16PS(b []byte, v uint16) []byte { return append(b, byte(v>>8), byte(v)) }

func appendUint24PS(b []byte, v uint32) []byte {
	return append(b, byte(v>>16), byte(v>>8), byte(v))
}

func TestProxysoakHelloMatchesTestUserSecret(t *testing.T) {
	now := time.Now()

	testSecret, err := ParseSecret("eea83146279491563539159a2da28b32837374756e30362d67652e7a7863762e62657374")
	if err != nil {
		t.Fatal(err)
	}

	hello := buildProxysoakHello(t, testSecret, now)

	conn := &memConn{buf: bytes.NewBuffer(hello)}
	_, err = fake.ReadClientHello(conn, testSecret.Key[:], testSecret.Host, 30*time.Second)
	if err != nil {
		t.Fatalf("ReadClientHello: %v", err)
	}
}

func TestMatchClientHelloMultiUserWithProxysoakPacket(t *testing.T) {
	now := time.Now()

	secrets := []Secret{
		mustParse("eecc18b2261265f8ad843498ff63534d807374756e30362d67652e7a7863762e62657374"),
		mustParse("ee2a5fc04e344798962b976923e35f506f7374756e30362d67652e7a7863762e62657374"),
		mustParse("eea83146279491563539159a2da28b32837374756e30362d67652e7a7863762e62657374"),
	}

	target := secrets[2]
	hello := buildProxysoakHello(t, target, now)

	server, client := netPipe(t)
	go func() {
		_, _ = client.Write(hello)
		_ = client.Close()
	}()

	rewind := newConnRewind(essentials.WrapNetConn(server))

	var lastErr error

	for _, secret := range secrets {
		rewind.Rewind()

		_, err := fake.ReadClientHello(rewind, secret.Key[:], secret.Host, 30*time.Second)
		if err == nil {
			return
		}

		lastErr = err
	}

	t.Fatalf("no secret matched: %v", lastErr)
}

func mustParse(s string) Secret {
	sec, err := ParseSecret(s)
	if err != nil {
		panic(err)
	}

	return sec
}
