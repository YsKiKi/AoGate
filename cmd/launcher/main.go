package main

import (
	"aogate/internal/launcher"
	"os"
)

func main() {
	launcher.Run(os.Args[1:])
}
