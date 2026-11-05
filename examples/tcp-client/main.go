package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/ninepeach/go-netx/tcp"
)

func main() {
	message := "hello from TCP client"
	if len(os.Args) > 1 {
		message = os.Args[1]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dialer := tcp.NewDialer(tcp.DialOptions{OnDial: func(e tcp.DialEvent) {
		log.Printf("dial %s took %s err=%v", e.Address, e.Duration, e.Err)
	}})
	conn, err := dialer.DialContext(ctx, "tcp", "127.0.0.1:9000")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	if _, err := fmt.Fprintln(conn, message); err != nil {
		log.Fatal(err)
	}
	response, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(response)
}
