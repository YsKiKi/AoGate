package main

import (
	"aogate/internal/packager"
	"os"
)

func main() {
	packager.Run(os.Args[1:])
}
