package php

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// StopLeftover stops any leftover PHP-FPM master for this version and removes
// the stale pool socket. Called before the supervisor starts FPM so a master
// that survived a daemon crash cannot keep the socket and report "stopped".
func StopLeftover(ver, serverRoot string) error {
	confPath := FPMConfigPath(ver, serverRoot)
	socketPath := FPMSocket(ver, serverRoot)
	pidPath := FPMPidPath(ver, serverRoot)

	cmds, err := listProcessCommands()
	if err != nil {
		log.Printf("php: list processes for leftover FPM %s: %v", ver, err)
		cmds = map[int]string{}
	}

	pids := map[int]struct{}{}
	for pid, cmd := range cmds {
		if pid > 1 && pid != os.Getpid() && strings.Contains(cmd, confPath) {
			pids[pid] = struct{}{}
		}
	}
	if pid, ok := readPidFile(pidPath); ok && pid != os.Getpid() {
		cmd := cmds[pid]
		if cmd == "" {
			cmd = commandForPID(pid)
		}
		if strings.Contains(cmd, confPath) || strings.Contains(cmd, "php-fpm") {
			pids[pid] = struct{}{}
		}
	}

	var remaining []int
	for pid := range pids {
		log.Printf("php: stopping leftover FPM %s (pid %d)", ver, pid)
		if !terminatePID(pid) {
			remaining = append(remaining, pid)
		}
	}

	_ = os.Remove(socketPath)

	if len(remaining) > 0 {
		return fmt.Errorf("php-fpm %s leftover still running: %v", ver, remaining)
	}
	return nil
}

func readPidFile(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || n <= 1 {
		return 0, false
	}
	return n, true
}

func listProcessCommands() (map[int]string, error) {
	out, err := exec.Command("ps", "-ax", "-o", "pid=,command=").Output()
	if err != nil {
		return nil, err
	}
	cmds := make(map[int]string)
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pidStr, cmd, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(pidStr))
		if err != nil || pid <= 0 {
			continue
		}
		cmds[pid] = strings.TrimSpace(cmd)
	}
	return cmds, nil
}

func commandForPID(pid int) string {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func terminatePID(pid int) bool {
	if pid <= 1 || pid == os.Getpid() {
		return true
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return true
	}
	_ = proc.Signal(syscall.SIGTERM)
	if pidGone(pid, proc, 5*time.Second) {
		return true
	}
	_ = proc.Kill()
	return pidGone(pid, proc, 2*time.Second)
}

func pidGone(pid int, proc *os.Process, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if proc.Signal(syscall.Signal(0)) != nil || pidIsZombie(pid) {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func pidIsZombie(pid int) bool {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	commEnd := strings.LastIndex(string(data), ") ")
	if commEnd < 0 || commEnd+2 >= len(data) {
		return false
	}
	return data[commEnd+2] == 'Z'
}
