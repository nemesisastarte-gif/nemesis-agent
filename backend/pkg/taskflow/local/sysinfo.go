package local

import (
	"os"
	"runtime"
	"strconv"
	"strings"
)

// readMemTotal 尽力而为地读取本机内存总量（Linux /proc/meminfo），
// 其它平台返回 0。
func readMemTotal() uint64 {
	if runtime.GOOS != "linux" {
		return 0
	}
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return kb * 1024
	}
	return 0
}

// readDiskTotal 尽力而为地读取 workspace 所在文件系统的总容量（statfs）。
// 非 Linux 或失败返回 0。
func readDiskTotal(path string) uint64 {
	if runtime.GOOS != "linux" {
		return 0
	}
	var st syscallStatfsT
	if err := statfs(path, &st); err != nil {
		return 0
	}
	return st.Blocks * uint64(st.Bsize)
}
