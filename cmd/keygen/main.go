package main

import (
	"aogate/internal/keygen"
	"os"
)

func main() {
	keygen.Run(os.Args[1:])
}
