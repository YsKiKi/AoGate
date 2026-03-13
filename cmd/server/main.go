package main

import (
	"aogate/internal/server"
	"os"
)

func main() {
	server.Run(os.Args[1:])
}
