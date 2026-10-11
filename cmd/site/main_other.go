//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() { fmt.Fprintln(os.Stderr, "remlink-site: Windows is required"); os.Exit(1) }
