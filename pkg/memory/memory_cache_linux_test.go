//
// Use and distribution licensed under the Apache license version 2.
//
// See the COPYING file in the root project directory for full text.
//

package memory_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/zededa/ghw/internal/config"
	"github.com/zededa/ghw/internal/sysfsfixture"
	"github.com/zededa/ghw/pkg/memory"
	"github.com/zededa/ghw/pkg/snapshot"
	"github.com/zededa/ghw/testdata"
)

// cachesFixture builds four CPUs with private L1 caches, an L2 per pair of
// CPUs and one L3. withID decides, per cache level, whether the kernel numbers
// that level's instances.
func cachesFixture(t *testing.T, withID func(level, cpu int) bool) string {
	t.Helper()
	id := func(level, cpu int, value int) string {
		if withID(level, cpu) {
			return strconv.Itoa(value)
		}
		return ""
	}
	var cpus []sysfsfixture.CPU
	for c := 0; c < 4; c++ {
		pair := "0-1"
		if c >= 2 {
			pair = "2-3"
		}
		cpus = append(cpus, sysfsfixture.CPU{ID: c, Caches: []sysfsfixture.Cache{
			{Index: 0, Level: "1", Type: "Data", ID: id(1, c, c), Size: "48K", Shared: strconv.Itoa(c)},
			{Index: 1, Level: "1", Type: "Instruction", ID: id(1, c, c), Size: "32K", Shared: strconv.Itoa(c)},
			{Index: 2, Level: "2", Type: "Unified", ID: id(2, c, c/2), Size: "2048K", Shared: pair},
			{Index: 3, Level: "3", Type: "Unified", ID: id(3, c, 0), Size: "30720K", Shared: "0-3"},
		}})
	}
	return sysfsfixture.Write(t, sysfsfixture.Tree{CPUs: cpus})
}

type cacheSummary struct {
	Level uint8
	Type  memory.CacheType
	ID    int
	Size  uint64
	CPUs  []uint32
}

func summarize(caches []*memory.Cache) []cacheSummary {
	out := make([]cacheSummary, len(caches))
	for i, c := range caches {
		out[i] = cacheSummary{c.Level, c.Type, c.ID, c.SizeBytes, c.LogicalProcessors}
	}
	return out
}

func wantCaches(id func(level, value int) int) []cacheSummary {
	var want []cacheSummary
	for c := 0; c < 4; c++ {
		want = append(want, cacheSummary{1, memory.CacheTypeInstruction, id(1, c), 32 * 1024, []uint32{uint32(c)}})
	}
	for c := 0; c < 4; c++ {
		want = append(want, cacheSummary{1, memory.CacheTypeData, id(1, c), 48 * 1024, []uint32{uint32(c)}})
	}
	want = append(want,
		cacheSummary{2, memory.CacheTypeUnified, id(2, 0), 2048 * 1024, []uint32{0, 1}},
		cacheSummary{2, memory.CacheTypeUnified, id(2, 1), 2048 * 1024, []uint32{2, 3}},
		cacheSummary{3, memory.CacheTypeUnified, id(3, 0), 30720 * 1024, []uint32{0, 1, 2, 3}},
	)
	return want
}

func caches(t *testing.T, root string) []cacheSummary {
	t.Helper()
	got, err := memory.Caches(config.ContextFromArgs(config.WithChroot(root)))
	if err != nil {
		t.Fatal(err)
	}
	return summarize(got)
}

func TestCaches(t *testing.T) {
	root := cachesFixture(t, func(int, int) bool { return true })
	want := wantCaches(func(_, value int) int { return value })
	if got := caches(t, root); !reflect.DeepEqual(got, want) {
		t.Errorf("Caches() =\n%v\nwant\n%v", got, want)
	}
}

// ARM device trees give caches no id: instances are told apart by the CPUs
// sharing them, and their ID is -1.
func TestCachesWithoutIDs(t *testing.T) {
	root := cachesFixture(t, func(int, int) bool { return false })
	want := wantCaches(func(int, int) int { return -1 })
	if got := caches(t, root); !reflect.DeepEqual(got, want) {
		t.Errorf("Caches() =\n%v\nwant\n%v", got, want)
	}
}

// One L2 numbered by the kernel and one not must stay two instances.
func TestCachesMixedIDs(t *testing.T) {
	root := cachesFixture(t, func(level, cpu int) bool { return level != 2 || cpu < 2 })
	var l2 []cacheSummary
	for _, c := range caches(t, root) {
		if c.Level == 2 {
			l2 = append(l2, c)
		}
	}
	want := []cacheSummary{
		{2, memory.CacheTypeUnified, 0, 2048 * 1024, []uint32{0, 1}},
		{2, memory.CacheTypeUnified, -1, 2048 * 1024, []uint32{2, 3}},
	}
	if !reflect.DeepEqual(l2, want) {
		t.Errorf("L2 caches = %v, want %v", l2, want)
	}
}

// A cache index whose level does not fit a Cache is skipped, not wrapped around.
func TestCachesLevelOutOfRange(t *testing.T) {
	root := sysfsfixture.Write(t, sysfsfixture.Tree{CPUs: []sysfsfixture.CPU{{ID: 0, Caches: []sysfsfixture.Cache{
		{Index: 0, Level: "1", Type: "Data", ID: "0", Size: "48K", Shared: "0"},
		{Index: 2, Level: "258", Type: "Unified", ID: "0", Size: "2048K", Shared: "0"},
	}}}})
	got := caches(t, root)
	if len(got) != 1 || got[0].Level != 1 {
		t.Errorf("caches = %+v, want only the L1", got)
	}
}

// An offline CPU is not counted even if its cache directory is still there.
func TestCachesSkipOfflineCPU(t *testing.T) {
	root := cachesFixture(t, func(int, int) bool { return true })
	if err := os.WriteFile(filepath.Join(root, "sys/devices/system/cpu/cpu3/online"), []byte("0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range caches(t, root) {
		for _, lp := range c.CPUs {
			if lp == 3 {
				t.Errorf("cache %+v lists offline CPU 3", c)
			}
		}
	}
}

// A blank shared_cpu_list must not merge private caches without ids.
func TestCachesBlankSharedList(t *testing.T) {
	var cpus []sysfsfixture.CPU
	for c := 0; c < 2; c++ {
		cpus = append(cpus, sysfsfixture.CPU{ID: c, Caches: []sysfsfixture.Cache{
			{Index: 2, Level: "2", Type: "Unified", Size: "1024K", Shared: " "},
		}})
	}
	got := caches(t, sysfsfixture.Write(t, sysfsfixture.Tree{CPUs: cpus}))
	if len(got) != 2 {
		t.Errorf("Caches() = %v, want two private L2 caches", got)
	}
}

// On the i7-1270P each 4-core E-module shares one L2.
func TestHybridSnapshotCaches(t *testing.T) {
	dir, err := testdata.SnapshotsDirectory()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := snapshot.UnpackInto(filepath.Join(dir, "linux-amd64-intel-i7-1270P.tar.gz"), root); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range caches(t, root) {
		if c.Level == 2 && reflect.DeepEqual(c.CPUs, []uint32{8, 9, 10, 11}) {
			found = true
			if c.ID != 4 {
				t.Errorf("E-module L2 ID = %d, want 4", c.ID)
			}
		}
	}
	if !found {
		t.Error("no L2 shared by CPUs 8-11")
	}
}

func cachesForNode(t *testing.T, root string, node int) []cacheSummary {
	t.Helper()
	got, err := memory.CachesForNode(config.ContextFromArgs(config.WithChroot(root)), node)
	if err != nil {
		t.Fatal(err)
	}
	return summarize(got)
}

// Each cache reports its own size: on the i7-1270P a P-core has a 48K L1d and
// a 32K L1i, an E-core a 32K L1d and a 64K L1i.
func TestCachesForNodeSizes(t *testing.T) {
	dir, err := testdata.SnapshotsDirectory()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := snapshot.UnpackInto(filepath.Join(dir, "linux-amd64-intel-i7-1270P.tar.gz"), root); err != nil {
		t.Fatal(err)
	}
	want := map[[2]uint32]uint64{ // {CPU, type} -> size
		{0, uint32(memory.CacheTypeData)}:        48 * 1024,
		{0, uint32(memory.CacheTypeInstruction)}: 32 * 1024,
		{8, uint32(memory.CacheTypeData)}:        32 * 1024,
		{8, uint32(memory.CacheTypeInstruction)}: 64 * 1024,
	}
	for _, c := range cachesForNode(t, root, 0) {
		if c.Level != 1 {
			continue
		}
		for _, cpu := range c.CPUs {
			if size, ok := want[[2]uint32{cpu, uint32(c.Type)}]; ok && c.Size != size {
				t.Errorf("CPU %d L1 %v size = %d, want %d", cpu, c.Type, c.Size, size)
			}
		}
	}
}

// A cache with kernel id 3 and one without an id whose CPU list reads "3" are
// two caches.
func TestCachesForNodeIDAndCPUListApart(t *testing.T) {
	root := sysfsfixture.Write(t, sysfsfixture.Tree{
		CPUs: []sysfsfixture.CPU{
			{ID: 0, Caches: []sysfsfixture.Cache{{Index: 2, Level: "2", Type: "Unified", ID: "3", Size: "1024K", Shared: "0"}}},
			{ID: 1, Caches: []sysfsfixture.Cache{{Index: 2, Level: "2", Type: "Unified", Size: "1024K", Shared: "3"}}},
		},
		Nodes: map[int]string{0: "0-1"},
	})
	if got := cachesForNode(t, root, 0); len(got) != 2 {
		t.Errorf("CachesForNode = %v, want two L2 caches", got)
	}
}
