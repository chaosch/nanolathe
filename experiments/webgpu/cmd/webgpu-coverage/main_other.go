//go:build !darwin

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "authored native coverage qualification requires the macOS GoGPU host")
	os.Exit(2)
}
