package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/ninepeach/go-netx"
	"github.com/ninepeach/go-netx/tcp"
)

func main() {
	ln, err := tcp.Listen(context.Background(), "tcp", ":8080", func(_ context.Context, c *tcp.Conn) {
		if err := serveHTTP(c); err != nil {
			log.Printf("conn %d: %v", c.ID(), err)
		}
	}, tcp.Options{})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("HTTP server listening on http://localhost:8080")
	if err := netx.NewServer(ln).Loop(); err != nil {
		log.Fatal(err)
	}
}

func serveHTTP(conn net.Conn) error {
	request, err := http.ReadRequest(bufio.NewReader(conn))
	if err != nil {
		return err
	}
	defer request.Body.Close()
	body := fmt.Sprintf("hello from go-netx\nmethod=%s\npath=%s\n", request.Method, request.URL.Path)
	response := &http.Response{
		StatusCode:    http.StatusOK,
		ProtoMajor:    1,
		ProtoMinor:    1,
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
		Header:        make(http.Header),
		Close:         true,
	}
	response.Header.Set("Content-Type", "text/plain; charset=utf-8")
	return response.Write(conn)
}
