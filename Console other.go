//go:build !windows

package main

// enableANSI на Linux и macOS ничего делать не нужно: терминал понимает ANSI.
func enableANSI() bool { return true }
