//go:build !windows

package core

// Заглушки управления процессом ядра для не-Windows сборок.
//
// Manager гоняет sing-box.exe отдельным процессом — это Windows-путь, и job
// object с Ctrl+Break у него насквозь платформенные. На Android процесса нет
// вовсе: ядро линкуется библиотекой в наш же процесс (TUN там выдаёт VpnService),
// а роль Manager играет обёртка в mobile/runner_android.go.
//
// Пакет всё равно должен собираться под android: оттуда берутся Paths, State и
// клиент Clash API, которыми пользуется backend/appcore. Сам Manager там просто
// никем не создаётся.

import "os/exec"

func applySysProcAttr(cmd *exec.Cmd) {}

func superviseChild(pid int) {}

func killProcessTree(pid int) {}

// requestGracefulStop сообщает, что мягкая остановка недоступна: вызывающий код
// сразу переходит к жёсткому завершению.
func requestGracefulStop(pid int) bool { return false }
