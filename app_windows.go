package main

import (
	"os/exec"
	"syscall"
)

// hideCmdWindow не даёт всплывать консольному окну при вызове sing-box.exe.
func hideCmdWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
