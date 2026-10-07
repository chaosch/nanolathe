//go:build !darwin

package main

import (
	"fmt"
	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/platform/ebitenapp"
)

func runMetalBattleBenchmark(_ Options, _ *contentSet, _ *battleSession, _ *client.Client, _ func(), _ func() any, _ ebitenapp.BenchmarkOptions) error {
	return fmt.Errorf("nanolathe: retained Metal benchmark requires macOS")
}
