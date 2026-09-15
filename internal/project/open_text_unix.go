//go:build darwin || linux

package project

import (
	"os"
	"syscall"
)

func openProjectTextFile(root *os.Root, relative string) (*os.File, error) {
	return root.OpenFile(relative, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
