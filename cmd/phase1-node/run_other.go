//go:build !windows

package main

import "errors"

func run([]string) error {
	return errors.New("Phase 1 Node POC requires Windows")
}
