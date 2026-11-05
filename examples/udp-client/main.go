// The UDP client is plain net: go-netx adds nothing a single dialled
// datagram socket needs.
package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"time"
)

func main() {
	message := "hello from UDP client"
	if len(os.Args) > 1 {
		message = os.Args[1]
	}
	conn, err := net.Dial("udp", "127.0.0.1:9001")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte(message)); err != nil {
		log.Fatal(err)
	}
	in := make([]byte, 65535)
	n, err := conn.Read(in)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(in[:n]))
}
