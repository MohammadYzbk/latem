//go:build !darwin && !linux

package main

import "os"

func openDiagnosticManifestDirectory(root *os.Root, relative string) (*os.File, error) {
	return root.Open(relative)
}
