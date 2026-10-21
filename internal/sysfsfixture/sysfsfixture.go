//
// Use and distribution licensed under the Apache license version 2.
//
// See the COPYING file in the root project directory for full text.
//

// Package sysfsfixture writes small fake sysfs and procfs trees describing a
// CPU topology, for tests that point ghw at them with a chroot.
package sysfsfixture

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/zededa/ghw/pkg/util"
)

// CPU describes one logical processor. An empty string field means the
// corresponding file does not exist.
type CPU struct {
	ID       int
	Package  string // topology/physical_package_id
	Core     string // topology/core_id
	Siblings string // topology/thread_siblings_list
	// Offline CPUs are present but not online: cpuN/online is 0 and they have
	// no topology, caches or frequencies, as on a real kernel.
	Offline  bool
	Caches   []Cache
	BaseKHz  string // cpufreq/base_frequency
	MaxKHz   string // cpufreq/cpuinfo_max_freq
	Capacity string // cpu_capacity
}

// Cache describes one cache/indexN directory of a CPU.
type Cache struct {
	Index  int
	Level  string
	Type   string
	ID     string
	Size   string
	Shared string // shared_cpu_list
}

// Tree describes the whole fixture.
type Tree struct {
	CPUs []CPU
	// Nodes maps a NUMA node to its cpulist. Nil means no node directory at
	// all, as on a kernel without NUMA.
	Nodes map[int]string
	// PMUCore and PMUAtom are the cpus files of the Intel hybrid core PMUs;
	// empty means the PMU is absent.
	PMUCore string
	PMUAtom string
	// CorePMU creates /sys/devices/cpu, the core PMU of a homogeneous part.
	CorePMU bool
}

// Write writes the tree under a new temporary directory and returns it, to be
// used as the chroot.
func Write(t testing.TB, tree Tree) string {
	t.Helper()
	root := t.TempDir()
	write := func(path, content string) {
		t.Helper()
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	optional := func(path, content string) {
		if content != "" {
			write(path, content)
		}
	}

	cpuDir := "sys/devices/system/cpu"
	var online, present []int
	var cpuinfo strings.Builder
	for _, c := range tree.CPUs {
		dir := fmt.Sprintf("%s/cpu%d", cpuDir, c.ID)
		present = append(present, c.ID)
		if c.Offline {
			write(dir+"/online", "0")
			continue
		}
		online = append(online, c.ID)
		write(dir+"/online", "1")
		optional(dir+"/topology/physical_package_id", c.Package)
		optional(dir+"/topology/core_id", c.Core)
		optional(dir+"/topology/thread_siblings_list", c.Siblings)
		optional(dir+"/cpufreq/base_frequency", c.BaseKHz)
		optional(dir+"/cpufreq/cpuinfo_max_freq", c.MaxKHz)
		optional(dir+"/cpu_capacity", c.Capacity)
		for _, cache := range c.Caches {
			idx := fmt.Sprintf("%s/cache/index%d", dir, cache.Index)
			optional(idx+"/level", cache.Level)
			optional(idx+"/type", cache.Type)
			optional(idx+"/id", cache.ID)
			optional(idx+"/size", cache.Size)
			optional(idx+"/shared_cpu_list", cache.Shared)
		}
		fmt.Fprintf(&cpuinfo, "processor\t: %d\nvendor_id\t: GenuineTest\nmodel name\t: Test CPU\nflags\t\t: fpu\n\n", c.ID)
	}
	write(cpuDir+"/online", List(online))
	write(cpuDir+"/present", List(present))
	write(cpuDir+"/possible", List(present))
	write("proc/cpuinfo", cpuinfo.String())

	if tree.Nodes != nil {
		nodes := make([]int, 0, len(tree.Nodes))
		for n := range tree.Nodes {
			nodes = append(nodes, n)
		}
		sort.Ints(nodes)
		for _, n := range nodes {
			nodeDir := fmt.Sprintf("sys/devices/system/node/node%d", n)
			write(nodeDir+"/cpulist", tree.Nodes[n])
			// Like the kernel, link each of the node's CPUs into its directory.
			cpus, err := util.ParseCPUList(tree.Nodes[n])
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range cpus {
				link := filepath.Join(root, nodeDir, fmt.Sprintf("cpu%d", c))
				if err := os.Symlink(fmt.Sprintf("../../cpu/cpu%d", c), link); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	optional("sys/devices/cpu_core/cpus", tree.PMUCore)
	optional("sys/devices/cpu_atom/cpus", tree.PMUAtom)
	if tree.CorePMU {
		write("sys/devices/cpu/type", "4")
	}
	return root
}

// List formats logical processor IDs as a comma-separated CPU list.
func List(ids []int) string {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = strconv.Itoa(id)
	}
	return strings.Join(s, ",")
}
