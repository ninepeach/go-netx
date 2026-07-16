package integration_test

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ninepeach/go-netx/tcp"
	"github.com/ninepeach/go-netx/udp"
)

func TestTCPServerAndClientsCommunicate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	server, err := tcp.Listen(ctx, "tcp", "127.0.0.1:0", tcp.HandlerFunc(func(_ context.Context, conn net.Conn) error {
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			if _, err := fmt.Fprintf(conn, "echo:%s\n", scanner.Text()); err != nil {
				return err
			}
		}
		return scanner.Err()
	}), tcp.ListenOptions{Server: tcp.Options{MaxConnections: 16}})
	if err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(ctx) }()

	const clients = 8
	const messages = 20
	var wg sync.WaitGroup
	errs := make(chan error, clients)
	for clientID := 0; clientID < clients; clientID++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			conn, err := tcp.Dial(ctx, "tcp", server.Addr().String())
			if err != nil {
				errs <- err
				return
			}
			defer conn.Close()
			reader := bufio.NewReader(conn)
			for messageID := 0; messageID < messages; messageID++ {
				message := fmt.Sprintf("client-%d-message-%d", id, messageID)
				if _, err := fmt.Fprintln(conn, message); err != nil {
					errs <- err
					return
				}
				response, err := reader.ReadString('\n')
				if err != nil {
					errs <- err
					return
				}
				if got, want := strings.TrimSpace(response), "echo:"+message; got != want {
					errs <- fmt.Errorf("got %q, want %q", got, want)
					return
				}
			}
		}(clientID)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	cancel()
	shutdownCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	if err := server.Shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}
	if err := <-serveDone; err != nil {
		t.Fatal(err)
	}
}

func TestUDPServerAndClientCommunicate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	server, err := udp.Listen("udp", "127.0.0.1:0", udp.HandlerFunc(func(ctx context.Context, writer udp.Writer, packet udp.Packet) error {
		response := append([]byte("echo:"), packet.Payload...)
		return writer.WritePacket(ctx, response, packet.RemoteAddr)
	}), udp.Options{MaxHandlers: 4})
	if err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(ctx) }()

	client, err := udp.Dial(ctx, "udp", server.Addr().String(), udp.ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	for i := 0; i < 50; i++ {
		message := fmt.Sprintf("packet-%d", i)
		exchangeCtx, stop := context.WithTimeout(ctx, time.Second)
		response, err := client.Exchange(exchangeCtx, []byte(message))
		stop()
		if err != nil {
			t.Fatal(err)
		}
		if got, want := string(response), "echo:"+message; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}

	cancel()
	shutdownCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	if err := server.Shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}
	if err := <-serveDone; err != nil {
		t.Fatal(err)
	}
}
