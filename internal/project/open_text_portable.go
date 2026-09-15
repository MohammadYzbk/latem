//go:build !darwin && !linux

package project

import "os"

func openProjectTextFile(root *os.Root, relative string) (*os.File, error) {
	return root.Open(relative)
}
