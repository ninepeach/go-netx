package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/ninepeach/go-netx"
)

func main() {
	s := netx.NewTCPServer(":1080", netx.TCPServerOptions{MaxConnections: 1024})
	s.OnConnect = serveSOCKS5
	s.OnStart = func(context.Context) error {
		log.Printf("SOCKS5 server listening on 127.0.0.1:1080")
		return nil
	}
	if err := s.Loop(); err != nil {
		log.Fatal(err)
	}
}

func serveSOCKS5(ctx context.Context, client net.Conn) error {
	if err := client.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	target, err := handshake(client)
	if err != nil {
		return err
	}
	upstream, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", target)
	if err != nil {
		_, _ = client.Write([]byte{5, 1, 0, 1, 0, 0, 0, 0, 0, 0})
		return err
	}
	defer upstream.Close()
	if _, err := client.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return err
	}
	_ = client.SetDeadline(time.Time{})
	return relay(client, upstream)
}

func handshake(conn net.Conn) (string, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return "", err
	}
	if header[0] != 5 {
		return "", errors.New("SOCKS5: unsupported version")
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return "", err
	}
	noAuth := false
	for _, method := range methods {
		if method == 0 {
			noAuth = true
			break
		}
	}
	if !noAuth {
		_, _ = conn.Write([]byte{5, 0xff})
		return "", errors.New("SOCKS5: no-auth method was not offered")
	}
	if _, err := conn.Write([]byte{5, 0}); err != nil {
		return "", err
	}
	request := make([]byte, 4)
	if _, err := io.ReadFull(conn, request); err != nil {
		return "", err
	}
	if request[0] != 5 || request[1] != 1 {
		return "", errors.New("SOCKS5: only CONNECT is supported")
	}
	var host string
	switch request[3] {
	case 1:
		address := make([]byte, net.IPv4len)
		if _, err := io.ReadFull(conn, address); err != nil {
			return "", err
		}
		host = net.IP(address).String()
	case 3:
		length := []byte{0}
		if _, err := io.ReadFull(conn, length); err != nil {
			return "", err
		}
		address := make([]byte, int(length[0]))
		if _, err := io.ReadFull(conn, address); err != nil {
			return "", err
		}
		host = string(address)
	case 4:
		address := make([]byte, net.IPv6len)
		if _, err := io.ReadFull(conn, address); err != nil {
			return "", err
		}
		host = net.IP(address).String()
	default:
		return "", fmt.Errorf("SOCKS5: unsupported address type %d", request[3])
	}
	port := make([]byte, 2)
	if _, err := io.ReadFull(conn, port); err != nil {
		return "", err
	}
	return net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(port)))), nil
}

func relay(left, right net.Conn) error {
	var wg sync.WaitGroup
	wg.Add(2)
	copyConn := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		if closer, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = closer.CloseWrite()
		}
	}
	go copyConn(left, right)
	go copyConn(right, left)
	wg.Wait()
	return nil
}
