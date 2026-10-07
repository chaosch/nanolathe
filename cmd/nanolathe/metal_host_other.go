//go:build !darwin

package main

import "fmt"

func runMetalPrototype(opts Options, cs *contentSet) error {
	return fmt.Errorf("nanolathe: retained Metal prototype requires macOS")
}
