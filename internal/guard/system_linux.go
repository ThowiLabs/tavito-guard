//go:build linux

package guard

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

type GPU struct {
	Name        string
	Busy        *float64
	Used, Total uint64
	Temperature *float64
}
type Snapshot struct {
	Time              time.Time
	Host              string
	CPU               float64
	Cores             int
	RAMUsed, RAMTotal uint64
	Uptime            float64
	GPUs              []GPU
}
type Disk struct {
	Path, Device, Type                              string
	Used, Total, Available, InodesUsed, InodesTotal uint64
}
type NetUsage struct {
	Name      string
	Addresses []string
	RX, TX    uint64
}
type Process struct {
	PID   int
	Name  string
	RSS   uint64
	Ticks uint64
	Start string
	CPU   float64
}

func parseCPU(data string) (total, idle uint64, err error) {
	f := strings.Fields(strings.SplitN(data, "\n", 2)[0])
	if len(f) < 5 || f[0] != "cpu" {
		return 0, 0, errors.New("contador CPU inválido")
	}
	// guest y guest_nice ya forman parte de user y nice.
	for i := 1; i < len(f) && i <= 8; i++ {
		v, e := strconv.ParseUint(f[i], 10, 64)
		if e != nil {
			return 0, 0, e
		}
		total += v
		if i == 4 || i == 5 {
			idle += v
		}
	}
	return total, idle, nil
}
func cpuCounters() (uint64, uint64, error) {
	b, e := os.ReadFile("/proc/stat")
	if e != nil {
		return 0, 0, e
	}
	return parseCPU(string(b))
}
func parseMemory(data string) (used, total uint64, err error) {
	values := map[string]uint64{}
	sc := bufio.NewScanner(strings.NewReader(data))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) >= 2 {
			v, e := strconv.ParseUint(f[1], 10, 64)
			if e == nil {
				values[strings.TrimSuffix(f[0], ":")] = v * 1024
			}
		}
	}
	total = values["MemTotal"]
	available, ok := values["MemAvailable"]
	if !ok {
		available = values["MemFree"] + values["Buffers"] + values["Cached"]
	}
	if total == 0 {
		return 0, 0, errors.New("MemTotal no disponible")
	}
	if available > total {
		available = total
	}
	return total - available, total, sc.Err()
}
func Collect(ctx context.Context) (Snapshot, error) {
	v := Snapshot{Time: time.Now(), Cores: runtime.NumCPU()}
	v.Host, _ = os.Hostname()
	t1, i1, e := cpuCounters()
	if e != nil {
		return v, e
	}
	timer := time.NewTimer(300 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return v, ctx.Err()
	case <-timer.C:
	}
	t2, i2, e := cpuCounters()
	if e != nil {
		return v, e
	}
	if t2 > t1 && i2 >= i1 {
		dt := t2 - t1
		di := i2 - i1
		if di <= dt {
			v.CPU = 100 * float64(dt-di) / float64(dt)
		}
	}
	b, e := os.ReadFile("/proc/meminfo")
	if e != nil {
		return v, e
	}
	v.RAMUsed, v.RAMTotal, e = parseMemory(string(b))
	if e != nil {
		return v, e
	}
	b, _ = os.ReadFile("/proc/uptime")
	if f := strings.Fields(string(b)); len(f) > 0 {
		v.Uptime, _ = strconv.ParseFloat(f[0], 64)
	}
	v.GPUs = readGPUs(ctx)
	return v, nil
}
func numberFile(path string) (uint64, bool) {
	b, e := os.ReadFile(path)
	if e != nil {
		return 0, false
	}
	v, e := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	return v, e == nil
}
func readGPUs(ctx context.Context) []GPU {
	var result []GPU
	paths, _ := filepath.Glob("/sys/class/drm/card[0-9]*/device")
	seen := map[string]bool{}
	for _, p := range paths {
		real, e := filepath.EvalSymlinks(p)
		if e != nil || seen[real] {
			continue
		}
		seen[real] = true
		vendor, _ := os.ReadFile(filepath.Join(p, "vendor"))
		device, _ := os.ReadFile(filepath.Join(p, "device"))
		brand := map[string]string{"0x1002": "AMD", "0x8086": "Intel", "0x10de": "NVIDIA"}[strings.TrimSpace(string(vendor))]
		if brand == "" {
			brand = "GPU"
		}
		g := GPU{Name: brand + " " + strings.TrimSpace(string(device))}
		if n, ok := numberFile(filepath.Join(p, "gpu_busy_percent")); ok {
			v := float64(n)
			g.Busy = &v
		}
		g.Used, _ = numberFile(filepath.Join(p, "mem_info_vram_used"))
		g.Total, _ = numberFile(filepath.Join(p, "mem_info_vram_total"))
		temps, _ := filepath.Glob(filepath.Join(p, "hwmon", "hwmon*", "temp1_input"))
		if len(temps) > 0 {
			if n, ok := numberFile(temps[0]); ok {
				v := float64(n) / 1000
				g.Temperature = &v
			}
		}
		result = append(result, g)
		if len(result) >= 8 {
			break
		}
	}
	if path, e := exec.LookPath("nvidia-smi"); e == nil {
		c, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		b, e := exec.CommandContext(c, path, "--query-gpu=name,utilization.gpu,memory.used,memory.total,temperature.gpu", "--format=csv,noheader,nounits").Output()
		if e == nil {
			var nvidia []GPU
			for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
				f := strings.Split(line, ",")
				if len(f) != 5 {
					continue
				}
				g := GPU{Name: strings.TrimSpace(f[0])}
				if v, e := strconv.ParseFloat(strings.TrimSpace(f[1]), 64); e == nil {
					g.Busy = &v
				}
				u, _ := strconv.ParseUint(strings.TrimSpace(f[2]), 10, 64)
				t, _ := strconv.ParseUint(strings.TrimSpace(f[3]), 10, 64)
				g.Used = u << 20
				g.Total = t << 20
				if v, e := strconv.ParseFloat(strings.TrimSpace(f[4]), 64); e == nil {
					g.Temperature = &v
				}
				nvidia = append(nvidia, g)
				if len(nvidia) >= 8 {
					break
				}
			}
			if len(nvidia) > 0 {
				var other []GPU
				for _, g := range result {
					if !strings.HasPrefix(g.Name, "NVIDIA") {
						other = append(other, g)
					}
				}
				result = append(other, nvidia...)
			}
		}
	}
	return result
}
func unescapeMount(s string) string {
	return strings.NewReplacer("\\040", " ", "\\011", "\t", "\\012", "\n", "\\134", "\\").Replace(s)
}
func Disks() ([]Disk, error) {
	b, e := os.ReadFile("/proc/self/mounts")
	if e != nil {
		return nil, e
	}
	ignore := map[string]bool{"proc": true, "sysfs": true, "devpts": true, "cgroup": true, "cgroup2": true, "securityfs": true, "debugfs": true, "tracefs": true, "mqueue": true, "pstore": true, "configfs": true, "hugetlbfs": true, "fusectl": true, "autofs": true}
	out := []Disk{}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || ignore[f[2]] {
			continue
		}
		p := unescapeMount(f[1])
		if seen[p] {
			continue
		}
		seen[p] = true
		fi, e := os.Stat(p)
		if e != nil || !fi.IsDir() {
			continue
		}
		var st unix.Statfs_t
		if unix.Statfs(p, &st) != nil || st.Blocks == 0 {
			continue
		}
		total := st.Blocks * uint64(st.Bsize)
		free := st.Bfree * uint64(st.Bsize)
		out = append(out, Disk{p, unescapeMount(f[0]), f[2], total - free, total, st.Bavail * uint64(st.Bsize), st.Files - st.Ffree, st.Files})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}
func Network() ([]NetUsage, error) {
	b, e := os.ReadFile("/proc/net/dev")
	if e != nil {
		return nil, e
	}
	counts := map[string][2]uint64{}
	for _, line := range strings.Split(string(b), "\n") {
		name, vals, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		f := strings.Fields(vals)
		if len(f) < 9 {
			continue
		}
		rx, _ := strconv.ParseUint(f[0], 10, 64)
		tx, _ := strconv.ParseUint(f[8], 10, 64)
		counts[strings.TrimSpace(name)] = [2]uint64{rx, tx}
	}
	ifaces, e := net.Interfaces()
	if e != nil {
		return nil, e
	}
	out := []NetUsage{}
	for _, n := range ifaces {
		u := NetUsage{Name: n.Name, RX: counts[n.Name][0], TX: counts[n.Name][1]}
		as, _ := n.Addrs()
		for _, a := range as {
			u.Addresses = append(u.Addresses, a.String())
		}
		out = append(out, u)
	}
	return out, nil
}
func parseProcess(pid int, stat string, pageSize uint64) (Process, error) {
	a := strings.Index(stat, "(")
	b := strings.LastIndex(stat, ")")
	if a < 0 || b <= a {
		return Process{}, errors.New("stat de proceso inválido")
	}
	f := strings.Fields(stat[b+1:])
	if len(f) < 22 {
		return Process{}, errors.New("stat de proceso incompleto")
	}
	user, e := strconv.ParseUint(f[11], 10, 64)
	if e != nil {
		return Process{}, e
	}
	sys, e := strconv.ParseUint(f[12], 10, 64)
	if e != nil {
		return Process{}, e
	}
	pages, _ := strconv.ParseUint(f[21], 10, 64)
	return Process{PID: pid, Name: stat[a+1 : b], RSS: pages * pageSize, Ticks: user + sys, Start: f[19]}, nil
}
func ReadProcess(pid int) (Process, error) {
	b, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if e != nil {
		return Process{}, e
	}
	return parseProcess(pid, string(b), uint64(os.Getpagesize()))
}
func processes() map[int]Process {
	entries, _ := os.ReadDir("/proc")
	out := map[int]Process{}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		p, err := ReadProcess(pid)
		if err == nil {
			out[pid] = p
		}
	}
	return out
}
func Processes(ctx context.Context, by string) ([]Process, error) {
	before := processes()
	t1, _, e := cpuCounters()
	if e != nil {
		return nil, e
	}
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
	}
	after := processes()
	t2, _, e := cpuCounters()
	if e != nil {
		return nil, e
	}
	out := []Process{}
	for pid, p := range after {
		if old, ok := before[pid]; ok && old.Start == p.Start && p.Ticks >= old.Ticks && t2 > t1 {
			p.CPU = 100 * float64(p.Ticks-old.Ticks) / float64(t2-t1)
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if by == "ram" {
			return out[i].RSS > out[j].RSS
		}
		return out[i].CPU > out[j].CPU
	})
	if len(out) > 30 {
		out = out[:30]
	}
	return out, nil
}
func Terminate(p Process) error {
	if p.PID <= 1 || p.PID == os.Getpid() || p.PID == os.Getppid() {
		return errors.New("proceso protegido")
	}
	fd, e := unix.PidfdOpen(p.PID, 0)
	if e != nil {
		return fmt.Errorf("no se pudo fijar el proceso (requiere kernel con pidfd): %w", e)
	}
	defer unix.Close(fd)
	current, e := ReadProcess(p.PID)
	if e != nil || current.Start != p.Start {
		return errors.New("el proceso terminó o cambió; actualiza la lista")
	}
	return unix.PidfdSendSignal(fd, unix.SIGTERM, nil, 0)
}
func Bytes(v uint64) string {
	for _, u := range []struct {
		d uint64
		s string
	}{{1 << 40, "TiB"}, {1 << 30, "GiB"}, {1 << 20, "MiB"}, {1 << 10, "KiB"}} {
		if v >= u.d {
			return fmt.Sprintf("%.1f %s", float64(v)/float64(u.d), u.s)
		}
	}
	return fmt.Sprintf("%d B", v)
}
func Percent(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(used) / float64(total)
}
