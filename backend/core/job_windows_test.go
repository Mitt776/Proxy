//go:build coretest

package core

import (
	"os/exec"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// IsProcessInJob не обёрнут в x/sys@v0.30 — вызываем напрямую из kernel32.
var procIsProcessInJob = windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob")

func isProcessInJob(proc windows.Handle) (bool, error) {
	var res int32
	r1, _, e := procIsProcessInJob.Call(uintptr(proc), 0, uintptr(unsafe.Pointer(&res)))
	if r1 == 0 {
		return false, e
	}
	return res != 0, nil
}

// TestSuperviseChildAssignsJob проверяет, что superviseChild реально помещает
// процесс в Job Object (гарантия убийства ядра при закрытии GUI).
func TestSuperviseChildAssignsJob(t *testing.T) {
	// Долгоживущий безобидный потомок: ping локалхоста.
	cmd := exec.Command("ping", "-n", "30", "127.0.0.1")
	applySysProcAttr(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("запуск потомка: %v", err)
	}
	defer func() { _ = cmd.Process.Kill() }()

	superviseChild(cmd.Process.Pid)

	ph, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION, false, uint32(cmd.Process.Pid))
	if err != nil {
		t.Fatalf("OpenProcess: %v", err)
	}
	defer windows.CloseHandle(ph)

	inJob, err := isProcessInJob(ph)
	if err != nil {
		t.Fatalf("IsProcessInJob: %v", err)
	}
	if !inJob {
		t.Fatal("процесс НЕ привязан к job object")
	}
	t.Log("✅ ядро привязывается к job object (kill-on-close работает)")
}
