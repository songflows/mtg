// proxysoak opens MTProxy FakeTLS+obfs2 and holds the session for a soak test.
package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/9seconds/mtg/v2/essentials"
	"github.com/9seconds/mtg/v2/mtglib"
	"github.com/9seconds/mtg/v2/mtglib/obfuscation"
)

const (
	typeHandshakeClient = 0x01
	randomLen           = 32
	// record + handshake header before client_random
	randomOffset = 1 + 2 + 2 + 1 + 3 + 2
)

func main() {
	addr := flag.String("addr", "stun06-ge.zxcv.best:3129", "proxy host:port")
	secretStr := flag.String("secret", "", "ee-secret (required)")
	label := flag.String("label", "client", "log prefix")
	duration := flag.Duration("duration", 2*time.Minute, "hold time after handshake")
	dc := flag.Int("dc", 2, "telegram DC in obfs handshake")
	flag.Parse()

	if *secretStr == "" {
		fmt.Fprintln(os.Stderr, "-secret is required")
		os.Exit(2)
	}

	secret, err := mtglib.ParseSecret(*secretStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse secret: %v\n", err)
		os.Exit(1)
	}

	raw, err := buildClientHello(secret.Key[:], secret.Host, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "build client hello: %v\n", err)
		os.Exit(1)
	}

	conn, err := net.DialTimeout("tcp", *addr, 10*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] dial: %v\n", *label, err)
		os.Exit(1)
	}
	defer conn.Close() //nolint: errcheck

	if err := conn.SetDeadline(time.Now().Add(15 * time.Second)); err != nil {
		fail(*label, "deadline", err)
	}

	if _, err := conn.Write(raw); err != nil {
		fail(*label, "write hello", err)
	}

	hdr := make([]byte, 5)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		fail(*label, "read tls header", err)
	}

	n := int(binary.BigEndian.Uint16(hdr[3:5]))
	rest := make([]byte, n)
	if _, err := io.ReadFull(conn, rest); err != nil {
		fail(*label, "read server hello", err)
	}

	obfs := obfuscation.Obfuscator{Secret: secret.Key[:]}
	obfsConn, err := obfs.SendHandshake(essentials.WrapNetConn(conn), *dc)
	if err != nil {
		fail(*label, "obfs handshake", err)
	}

	if err := conn.SetDeadline(time.Time{}); err != nil {
		fail(*label, "clear deadline", err)
	}

	fmt.Printf("[%s] handshake ok, holding %s\n", *label, duration)

	deadline := time.Now().Add(*duration)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()

		tick := time.NewTicker(10 * time.Second)
		defer tick.Stop()

		for {
			select {
			case <-tick.C:
				fmt.Printf("[%s] alive %s\n", *label, time.Now().Format("15:04:05"))
			case <-time.After(time.Until(deadline)):
				return
			}
		}
	}()

	go func() {
		defer wg.Done()

		ping := make([]byte, 64)
		for time.Now().Before(deadline) {
			_ = obfsConn.SetReadDeadline(time.Now().Add(20 * time.Second))
			if _, err := obfsConn.Read(ping); err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					continue
				}

				fmt.Printf("[%s] read ended: %v at %s\n", *label, err, time.Now().Format("15:04:05"))
				return
			}
		}
	}()

	wg.Wait()
	fmt.Printf("[%s] done\n", *label)
}

func fail(label, step string, err error) {
	fmt.Fprintf(os.Stderr, "[%s] %s: %v\n", label, step, err)
	os.Exit(1)
}

func buildClientHello(secret []byte, hostname string, now time.Time) ([]byte, error) {
	sessionID := make([]byte, 32)
	if _, err := rand.Read(sessionID); err != nil {
		return nil, err
	}

	const cipherSuite = 0x1301

	ext := buildSNI(hostname)
	bodyLen := 2 + randomLen + 1 + len(sessionID) + 2 + 2 + 1 + 1 + 2 + len(ext)
	// ClientHello body length (client_version .. extensions), excluding type+uint24.
	recordLen := 1 + 3 + bodyLen

	var full []byte
	full = append(full, 0x16, 3, 1)
	full = appendUint16(full, uint16(recordLen))
	full = append(full, typeHandshakeClient)
	full = appendUint24(full, uint32(bodyLen))
	full = appendUint16(full, 0x0303)
	full = append(full, make([]byte, randomLen)...)
	full = append(full, byte(len(sessionID)))
	full = append(full, sessionID...)
	full = appendUint16(full, 2)
	full = appendUint16(full, cipherSuite)
	full = append(full, 1, 0)
	full = appendUint16(full, uint16(len(ext)))
	full = append(full, ext...)

	digest := hmac.New(sha256.New, secret)
	digest.Write(full[:randomOffset])
	digest.Write(make([]byte, randomLen))
	digest.Write(full[randomOffset+randomLen:])

	sum := digest.Sum(nil)
	ts := make([]byte, 4)
	binary.LittleEndian.PutUint32(ts, uint32(now.Unix()))
	for i := 0; i < randomLen-4; i++ {
		full[randomOffset+i] = sum[i]
	}
	for i := 0; i < 4; i++ {
		full[randomOffset+randomLen-4+i] = sum[randomLen-4+i] ^ ts[i]
	}

	return full, nil
}

func buildSNI(hostname string) []byte {
	host := []byte(hostname)
	var b []byte
	b = appendUint16(b, 0)
	listLen := 1 + 2 + len(host)
	b = appendUint16(b, uint16(listLen+2))
	b = appendUint16(b, uint16(listLen))
	b = append(b, 0)
	b = appendUint16(b, uint16(len(host)))
	b = append(b, host...)

	return b
}

func appendUint16(b []byte, v uint16) []byte {
	return append(b, byte(v>>8), byte(v))
}

func appendUint24(b []byte, v uint32) []byte {
	return append(b, byte(v>>16), byte(v>>8), byte(v))
}
