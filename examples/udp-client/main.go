package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/ninepeach/netx/udp"
)

func main() {
	message := "hello from UDP client"
	if len(os.Args) > 1 {
		message = os.Args[1]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := udp.Dial(ctx, "udp", "127.0.0.1:9001", udp.ClientOptions{})
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()
	response, err := client.Exchange(ctx, []byte(message))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(response))
}
