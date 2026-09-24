package tsdb

import (
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// В Windows нет SIGHUP, и POST /-/reload у VictoriaMetrics ничего не делает. Поэтому
// она сама перечитывает файл конфигурации сбора, если он изменился; Reconciler
// по-прежнему дожидается роста vm_promscrape_config_reloads_total.
var platformArgs = []string{"-promscrape.configCheckInterval=2s"}

// Отдельная группа процессов нужна, чтобы CTRL_BREAK_EVENT получила только TSDB:
// Go-рантайм VictoriaMetrics превращает его в SIGINT и штатно сбрасывает данные.
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

// Job object с KILL_ON_JOB_CLOSE: Windows завершает TSDB, когда закрывается
// последний дескриптор job, то есть вместе с HomeDeck, даже при аварийном выходе.
var job = sync.OnceValue(func() windows.Handle {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE},
	}
	if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(h)
		return 0
	}
	return h
})

func bindToParent(p *os.Process) {
	j := job()
	if j == 0 {
		return
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(p.Pid))
	if err != nil {
		return
	}
	defer func() { _ = windows.CloseHandle(h) }()
	_ = windows.AssignProcessToJobObject(j, h)
}

func stopProcess(p *os.Process) {
	if windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(p.Pid)) != nil {
		_ = p.Kill()
	}
}

func killProcessGroup(p *os.Process) {
	_ = p.Kill()
}
