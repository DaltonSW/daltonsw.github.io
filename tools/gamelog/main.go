package main

import (
	"flag"
	"fmt"
	"os"

	"go.dalton.dog/gamelog/internal/server"
)

func main() {
	port := flag.Int("port", server.DefaultPort, "port to listen on (127.0.0.1 only)")
	flag.Parse()
	if err := server.Run(*port); err != nil {
		fmt.Fprintln(os.Stderr, "gamelog:", err)
		os.Exit(1)
	}
}
