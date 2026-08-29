package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "phase1-node: %v\n", err)
		os.Exit(1)
	}
}
