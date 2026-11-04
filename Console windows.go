//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// enableANSI включает в консоли Windows обработку ANSI-последовательностей
// (флаг ENABLE_VIRTUAL_TERMINAL_PROCESSING = 0x0004). Без него старая консоль
// (cmd.exe / conhost) печатает коды как текст "←[2K" вместо управления курсором.
// Возвращает true, если режим включён.
func enableANSI() bool {
	kernel := syscall.NewLazyDLL("kernel32.dll")
	getMode := kernel.NewProc("GetConsoleMode")
	setMode := kernel.NewProc("SetConsoleMode")

	h, err := syscall.GetStdHandle(syscall.STD_OUTPUT_HANDLE)
	if err != nil {
		return false
	}
	var mode uint32
	if r, _, _ := getMode.Call(uintptr(h), uintptr(unsafe.Pointer(&mode))); r == 0 {
		return false // вывод перенаправлен в файл или это не консоль
	}
	r, _, _ := setMode.Call(uintptr(h), uintptr(mode|0x0004))
	return r != 0
}
