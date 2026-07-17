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
)

func main() {
	s := netx.NewTCPServer(":8080")
	s.OnConnect = serveHTTP
	s.OnStart = func(context.Context) error {
		log.Printf("HTTP server listening on http://localhost:8080")
		return nil
	}
	if err := s.Loop(); err != nil {
		log.Fatal(err)
	}
}

func serveHTTP(_ context.Context, conn net.Conn) error {
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
