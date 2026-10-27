//
// Use and distribution licensed under the Apache license version 2.
//
// See the COPYING file in the root project directory for full text.
//

package cpu_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/zededa/ghw/internal/config"
	"github.com/zededa/ghw/internal/sysfsfixture"
	"github.com/zededa/ghw/pkg/cpu"
	"github.com/zededa/ghw/pkg/memory"
	"github.com/zededa/ghw/pkg/snapshot"
	"github.com/zededa/ghw/testdata"
)

// coresOf returns each core's logical processors, keyed by core ID, for the
// processor with the given ID.
func coresOf(t *testing.T, info *cpu.Info, procID int) map[int][]int {
	t.Helper()
	for _, p := range info.Processors {
		if p.ID != procID {
			continue
		}
		cores := map[int][]int{}
		for _, c := range p.Cores {
			if _, dup := cores[c.ID]; dup {
				t.Fatalf("processor %d has two cores with ID %d", procID, c.ID)
			}
			cores[c.ID] = c.LogicalProcessors
		}
		return cores
	}
	t.Fatalf("no processor %d in %v", procID, info.Processors)
	return nil
}

// An ARM device tree numbers cores per cluster, so core_id repeats within one
// package; the sibling lists tell the cores apart.
func TestCoresAcrossClusters(t *testing.T) {
	var cpus []sysfsfixture.CPU
	for id := 0; id < 8; id++ {
		cpus = append(cpus, sysfsfixture.CPU{
			ID:       id,
			Package:  "0",
			Core:     strconv.Itoa(id % 4),
			Siblings: strconv.Itoa(id),
		})
	}
	root := sysfsfixture.Write(t, sysfsfixture.Tree{CPUs: cpus})

	info, err := cpu.New(config.WithChroot(root))
	if err != nil {
		t.Fatal(err)
	}
	want := map[int][]int{0: {0}, 1: {1}, 2: {2}, 3: {3}, 4: {4}, 5: {5}, 6: {6}, 7: {7}}
	if got := coresOf(t, info, 0); !reflect.DeepEqual(got, want) {
		t.Errorf("cores = %v, want %v", got, want)
	}
	if info.TotalCores != 8 {
		t.Errorf("TotalCores = %d, want 8", info.TotalCores)
	}
}

// Where core_id is unique within the package, as on x86, it is kept.
func TestCoreIDsKeptOnX86(t *testing.T) {
	root := sysfsfixture.Write(t, sysfsfixture.Tree{CPUs: []sysfsfixture.CPU{
		{ID: 0, Package: "0", Core: "0", Siblings: "0,2"},
		{ID: 1, Package: "0", Core: "1", Siblings: "1,3"},
		{ID: 2, Package: "0", Core: "0", Siblings: "0,2"},
		{ID: 3, Package: "0", Core: "1", Siblings: "1,3"},
		{ID: 4, Package: "1", Core: "0", Siblings: "4"},
	}})

	info, err := cpu.New(config.WithChroot(root))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := coresOf(t, info, 0), map[int][]int{0: {0, 2}, 1: {1, 3}}; !reflect.DeepEqual(got, want) {
		t.Errorf("package 0 cores = %v, want %v", got, want)
	}
	if got, want := coresOf(t, info, 1), map[int][]int{0: {4}}; !reflect.DeepEqual(got, want) {
		t.Errorf("package 1 cores = %v, want %v", got, want)
	}
}

// In a renumbered package, a CPU whose siblings cannot be read has an unknown
// core rather than its kernel core_id, which another core now uses. Unknown
// cores are not known to be shared, so each is a core of its own.
func TestClusteredPackageCPUsWithoutSiblings(t *testing.T) {
	var cpus []sysfsfixture.CPU
	for id := 0; id < 8; id++ {
		cpus = append(cpus, sysfsfixture.CPU{
			ID: id, Package: "0", Core: strconv.Itoa(id % 4), Siblings: strconv.Itoa(id),
		})
	}
	cpus[5].Siblings, cpus[6].Siblings = "", ""
	root := sysfsfixture.Write(t, sysfsfixture.Tree{CPUs: cpus, Nodes: map[int]string{0: "0-7"}})
	info, err := cpu.New(config.WithChroot(root))
	if err != nil {
		t.Fatal(err)
	}
	cores, err := cpu.CoresForNode(config.ContextFromArgs(config.WithChroot(root)), 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"0:[0]", "1:[1]", "2:[2]", "3:[3]", "4:[4]", "-1:[5]", "-1:[6]", "7:[7]"}
	if got := coreList(info.Processors[0].Cores); !reflect.DeepEqual(got, want) {
		t.Errorf("Info cores = %v, want %v", got, want)
	}
	if got := coreList(cores); !reflect.DeepEqual(got, want) {
		t.Errorf("CoresForNode = %v, want %v", got, want)
	}
}

// CPUs whose core_id and siblings cannot be read are not merged into one core
// either.
func TestCPUsWithoutCoreID(t *testing.T) {
	cpus := []sysfsfixture.CPU{
		{ID: 0, Package: "0", Core: "0", Siblings: "0"},
		{ID: 1, Package: "0"},
		{ID: 2, Package: "0"},
	}
	info, err := cpu.New(config.WithChroot(sysfsfixture.Write(t, sysfsfixture.Tree{CPUs: cpus})))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"0:[0]", "-1:[1]", "-1:[2]"}
	if got := coreList(info.Processors[0].Cores); !reflect.DeepEqual(got, want) {
		t.Errorf("cores = %v, want %v", got, want)
	}
}

// coreList lists cores as "ID:[logical processors]", by first logical processor.
func coreList(cores []*cpu.ProcessorCore) []string {
	sorted := slices.Clone(cores)
	slices.SortFunc(sorted, func(a, b *cpu.ProcessorCore) int {
		return a.LogicalProcessors[0] - b.LogicalProcessors[0]
	})
	var out []string
	for _, c := range sorted {
		out = append(out, fmt.Sprintf("%d:%v", c.ID, c.LogicalProcessors))
	}
	return out
}

// Renumbering is per package: a package whose core_id is unique keeps it.
func TestOneOfTwoPackagesClustered(t *testing.T) {
	root := sysfsfixture.Write(t, sysfsfixture.Tree{CPUs: []sysfsfixture.CPU{
		{ID: 0, Package: "0", Core: "0", Siblings: "0"},
		{ID: 1, Package: "0", Core: "1", Siblings: "1"},
		{ID: 2, Package: "0", Core: "0", Siblings: "2"},
		{ID: 3, Package: "0", Core: "1", Siblings: "3"},
		{ID: 4, Package: "1", Core: "0", Siblings: "4-5"},
		{ID: 5, Package: "1", Core: "0", Siblings: "4-5"},
		{ID: 6, Package: "1", Core: "1", Siblings: "6-7"},
		{ID: 7, Package: "1", Core: "1", Siblings: "6-7"},
	}})
	info, err := cpu.New(config.WithChroot(root))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := coresOf(t, info, 0), map[int][]int{0: {0}, 1: {1}, 2: {2}, 3: {3}}; !reflect.DeepEqual(got, want) {
		t.Errorf("package 0 cores = %v, want %v", got, want)
	}
	if got, want := coresOf(t, info, 1), map[int][]int{0: {4, 5}, 1: {6, 7}}; !reflect.DeepEqual(got, want) {
		t.Errorf("package 1 cores = %v, want %v", got, want)
	}
}

// CoresForNode identifies cores as Info does: clusters are told apart.
func TestCoresForNodeAcrossClusters(t *testing.T) {
	var cpus []sysfsfixture.CPU
	for id := 0; id < 8; id++ {
		cpus = append(cpus, sysfsfixture.CPU{
			ID: id, Package: "0", Core: strconv.Itoa(id % 4), Siblings: strconv.Itoa(id),
		})
	}
	ctx := config.ContextFromArgs(config.WithChroot(sysfsfixture.Write(t,
		sysfsfixture.Tree{CPUs: cpus, Nodes: map[int]string{0: "0-7"}})))
	cores, err := cpu.CoresForNode(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(cores) != 8 {
		t.Errorf("CoresForNode = %v, want 8 single-thread cores", cores)
	}
}

// When a package's clusters are separate NUMA nodes, CoresForNode numbers each
// node's cores as Info does: the package is clustered whichever subset of its
// CPUs is asked for.
func TestCoresForNodeClustersAsNodes(t *testing.T) {
	var cpus []sysfsfixture.CPU
	for id := 0; id < 8; id++ {
		cpus = append(cpus, sysfsfixture.CPU{
			ID: id, Package: "0", Core: strconv.Itoa(id % 4), Siblings: strconv.Itoa(id),
		})
	}
	root := sysfsfixture.Write(t, sysfsfixture.Tree{CPUs: cpus, Nodes: map[int]string{0: "0-3", 1: "4-7"}})
	info, err := cpu.New(config.WithChroot(root))
	if err != nil {
		t.Fatal(err)
	}
	infoCores := map[int][]int{}
	for _, p := range info.Processors {
		for _, c := range p.Cores {
			infoCores[c.ID] = c.LogicalProcessors
		}
	}
	ctx := config.ContextFromArgs(config.WithChroot(root))
	for _, node := range []int{0, 1} {
		cores, err := cpu.CoresForNode(ctx, node)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cores {
			if !reflect.DeepEqual(infoCores[c.ID], c.LogicalProcessors) {
				t.Errorf("node %d: core %d has %v, Info's core %d has %v",
					node, c.ID, c.LogicalProcessors, c.ID, infoCores[c.ID])
			}
		}
	}
}

// Two packages in one NUMA node reuse core IDs; their cores stay apart.
func TestCoresForNodeTwoPackages(t *testing.T) {
	tree := sysfsfixture.Tree{
		CPUs: []sysfsfixture.CPU{
			{ID: 0, Package: "0", Core: "0", Siblings: "0"},
			{ID: 1, Package: "1", Core: "0", Siblings: "1"},
		},
		Nodes: map[int]string{0: "0-1"},
	}
	ctx := config.ContextFromArgs(config.WithChroot(sysfsfixture.Write(t, tree)))
	cores, err := cpu.CoresForNode(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(cores) != 2 {
		t.Errorf("CoresForNode = %v, want 2 cores", cores)
	}
}

// A 13th-gen Core i5-1335UE: two P-cores with two threads each (CPUs 0-3,
// under the cpu_core PMU) and eight E-cores (CPUs 4-11, under cpu_atom), each
// module of four E-cores sharing an L2. Turbo is off on that machine, so each
// CPU's maximum frequency is its base frequency.
func TestHybridPMUSnapshot(t *testing.T) {
	info, err := cpu.New(config.WithChroot(unpackSnapshot(t, "linux-amd64-intel-i5-1335UE.tar.gz")))
	if err != nil {
		t.Fatal(err)
	}
	if info.TotalCores != 10 || info.TotalHardwareThreads != 12 {
		t.Errorf("TotalCores, TotalHardwareThreads = %d, %d; want 10, 12",
			info.TotalCores, info.TotalHardwareThreads)
	}
	var lps []string
	for _, lp := range info.LogicalProcessors {
		lps = append(lps, fmt.Sprintf("%d:%d:%s:%d", lp.ID, lp.CoreID, lp.CoreType, lp.MaxFrequencyKHz))
	}
	wantLPs := []string{
		"0:0:performance:800000", "1:0:performance:800000",
		"2:4:performance:800000", "3:4:performance:800000",
		"4:8:efficiency:600000", "5:9:efficiency:600000",
		"6:10:efficiency:600000", "7:11:efficiency:600000",
		"8:12:efficiency:600000", "9:13:efficiency:600000",
		"10:14:efficiency:600000", "11:15:efficiency:600000",
	}
	if !reflect.DeepEqual(lps, wantLPs) {
		t.Errorf("logical processors (ID:core:type:max kHz) =\n%v\nwant\n%v", lps, wantLPs)
	}
	var caches []string
	for _, c := range info.Caches {
		if c.Level == 2 || c.Type == memory.CacheTypeData {
			caches = append(caches, fmt.Sprintf("L%d %s %dK %v", c.Level, c.Type, c.SizeBytes/1024, c.LogicalProcessors))
		}
	}
	wantCaches := []string{
		"L1 Data 48K [0 1]", "L1 Data 48K [2 3]",
		"L1 Data 32K [4]", "L1 Data 32K [5]", "L1 Data 32K [6]", "L1 Data 32K [7]",
		"L1 Data 32K [8]", "L1 Data 32K [9]", "L1 Data 32K [10]", "L1 Data 32K [11]",
		"L2 Unified 1280K [0 1]", "L2 Unified 1280K [2 3]",
		"L2 Unified 2048K [4 5 6 7]", "L2 Unified 2048K [8 9 10 11]",
	}
	if !reflect.DeepEqual(caches, wantCaches) {
		t.Errorf("L1d and L2 caches =\n%v\nwant\n%v", caches, wantCaches)
	}
}

// An NVIDIA Jetson AGX Orin: its kernel reports each cluster of four cores as
// a package, so core IDs repeat across packages but not within one and are
// kept. Every CPU has capacity 1024: a homogeneous part. The kernel numbers no
// cache, so cache instances are told apart by the CPUs sharing them. The CPU
// model comes from the DeviceTree.
func TestARMClusterPackagesSnapshot(t *testing.T) {
	info, err := cpu.New(config.WithChroot(unpackSnapshot(t, "linux-arm64-nvidia-jetson-agx-orin.tar.gz")))
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Processors) != 3 {
		t.Fatalf("Processors = %v, want 3", info.Processors)
	}
	for _, p := range info.Processors {
		if p.Model != "nvidia,tegra234" {
			t.Errorf("package %d model = %q, want the SoC from the DeviceTree", p.ID, p.Model)
		}
		first := p.ID * 4
		want := []string{
			fmt.Sprintf("0:[%d]", first), fmt.Sprintf("1:[%d]", first+1),
			fmt.Sprintf("2:[%d]", first+2), fmt.Sprintf("3:[%d]", first+3),
		}
		if got := coreList(p.Cores); !reflect.DeepEqual(got, want) {
			t.Errorf("package %d cores = %v, want %v", p.ID, got, want)
		}
	}
	for _, lp := range info.LogicalProcessors {
		if lp.ProcessorID != lp.ID/4 || lp.CoreID != lp.ID%4 || lp.CoreType != cpu.CoreTypeUniform || lp.MaxFrequencyKHz != 2201600 {
			t.Errorf("logical processor %+v", lp)
		}
	}
	var l3 []string
	for _, c := range info.Caches {
		if c.ID != -1 {
			t.Errorf("cache %+v has a kernel ID", c)
		}
		if c.Level == 3 {
			l3 = append(l3, fmt.Sprintf("%dK %v", c.SizeBytes/1024, c.LogicalProcessors))
		}
	}
	if want := []string{"2048K [0 1 2 3]", "2048K [4 5 6 7]", "2048K [8 9 10 11]"}; !reflect.DeepEqual(l3, want) {
		t.Errorf("L3 caches = %v, want %v", l3, want)
	}
	if len(info.Caches) != 39 {
		t.Errorf("%d caches, want 39: L1i, L1d and L2 per CPU and an L3 per cluster", len(info.Caches))
	}
}

// On the i7-1270P the E-cores 8-11 share an L2 but are four cores of their own.
func TestHybridSnapshotCores(t *testing.T) {
	info, err := cpu.New(config.WithChroot(unpackSnapshot(t, "linux-amd64-intel-i7-1270P.tar.gz")))
	if err != nil {
		t.Fatal(err)
	}
	cores := map[int][]int{}
	for _, p := range info.Processors {
		for _, c := range p.Cores {
			cores[c.ID] = c.LogicalProcessors
		}
	}
	for _, lp := range []int{8, 9, 10, 11} {
		found := false
		for _, lps := range cores {
			if reflect.DeepEqual(lps, []int{lp}) {
				found = true
			}
		}
		if !found {
			t.Errorf("E-core CPU %d is not a core of its own: %v", lp, cores)
		}
	}
	if info.TotalCores != 12 || info.TotalHardwareThreads != 16 {
		t.Errorf("TotalCores, TotalHardwareThreads = %d, %d; want 12, 16",
			info.TotalCores, info.TotalHardwareThreads)
	}
}

func unpackSnapshot(t *testing.T, name string) string {
	t.Helper()
	dir, err := testdata.SnapshotsDirectory()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := snapshot.UnpackInto(filepath.Join(dir, name), root); err != nil {
		t.Fatal(err)
	}
	return root
}

// twoSocketTree is 2 packages x 2 cores x 2 threads, one NUMA node per
// package, with frequencies on CPU 0 only.
func twoSocketTree() sysfsfixture.Tree {
	var cpus []sysfsfixture.CPU
	for id := 0; id < 8; id++ {
		pkg, core := id/4, (id%4)/2
		first := pkg*4 + core*2
		c := sysfsfixture.CPU{
			ID:       id,
			Package:  strconv.Itoa(pkg),
			Core:     strconv.Itoa(core),
			Siblings: strconv.Itoa(first) + "-" + strconv.Itoa(first+1),
		}
		if id == 0 {
			c.BaseKHz, c.MaxKHz = "2100000", "3500000"
		}
		cpus = append(cpus, c)
	}
	return sysfsfixture.Tree{CPUs: cpus, Nodes: map[int]string{0: "0-3", 1: "4-7"}}
}

func logicalProcessors(t *testing.T, tree sysfsfixture.Tree) *cpu.Info {
	t.Helper()
	info, err := cpu.New(config.WithChroot(sysfsfixture.Write(t, tree)))
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func TestLogicalProcessors(t *testing.T) {
	info := logicalProcessors(t, twoSocketTree())
	var want []*cpu.LogicalProcessor
	for id := 0; id < 8; id++ {
		lp := &cpu.LogicalProcessor{ID: id, ProcessorID: id / 4, CoreID: (id % 4) / 2, NodeID: id / 4}
		if id == 0 {
			lp.BaseFrequencyKHz, lp.MaxFrequencyKHz = 2100000, 3500000
		}
		want = append(want, lp)
	}
	if !reflect.DeepEqual(info.LogicalProcessors, want) {
		t.Errorf("LogicalProcessors =\n%v\nwant\n%v", info.LogicalProcessors, want)
	}
	if info.OfflineLogicalProcessors != nil {
		t.Errorf("OfflineLogicalProcessors = %v, want none", info.OfflineLogicalProcessors)
	}
}

// A kernel without NUMA has no node directory: every CPU is on node 0.
func TestLogicalProcessorsNoNUMA(t *testing.T) {
	tree := twoSocketTree()
	tree.Nodes = nil
	for _, lp := range logicalProcessors(t, tree).LogicalProcessors {
		if lp.NodeID != 0 {
			t.Errorf("CPU %d NodeID = %d, want 0", lp.ID, lp.NodeID)
		}
	}
}

// A CPU no node lists is on an unknown node, not on node 0.
func TestLogicalProcessorsNUMAHole(t *testing.T) {
	tree := twoSocketTree()
	tree.Nodes = map[int]string{0: "0-3", 1: "4-6"}
	for _, lp := range logicalProcessors(t, tree).LogicalProcessors {
		want := lp.ID / 4
		if lp.ID == 7 {
			want = -1
		}
		if lp.NodeID != want {
			t.Errorf("CPU %d NodeID = %d, want %d", lp.ID, lp.NodeID, want)
		}
	}
}

// A CPU whose topology cannot be read has unknown package and core.
func TestLogicalProcessorsUnreadableTopology(t *testing.T) {
	tree := twoSocketTree()
	tree.CPUs[2].Package, tree.CPUs[2].Core, tree.CPUs[2].Siblings = "", "", ""
	for _, lp := range logicalProcessors(t, tree).LogicalProcessors {
		wantPkg, wantCore := lp.ID/4, (lp.ID%4)/2
		if lp.ID == 2 {
			wantPkg, wantCore = -1, -1
		}
		if lp.ProcessorID != wantPkg || lp.CoreID != wantCore {
			t.Errorf("CPU %d = package %d core %d, want package %d core %d",
				lp.ID, lp.ProcessorID, lp.CoreID, wantPkg, wantCore)
		}
	}
}

// physical_package_id -1 is the kernel saying there is no package
// information: one package, 0.
func TestPackageIDMinusOne(t *testing.T) {
	tree := twoSocketTree()
	for i := range tree.CPUs {
		tree.CPUs[i].Package = "-1"
	}
	tree.CPUs = tree.CPUs[:4]
	info := logicalProcessors(t, tree)
	for _, lp := range info.LogicalProcessors {
		if lp.ProcessorID != 0 {
			t.Errorf("CPU %d ProcessorID = %d, want 0", lp.ID, lp.ProcessorID)
		}
	}
	if len(info.Processors) != 1 || info.Processors[0].ID != 0 {
		t.Errorf("Processors = %v, want one with ID 0", info.Processors)
	}
}

// A device-tree board with no package information and core_id repeating per
// cluster is still renumbered: its one package, 0, is clustered.
func TestPackageIDMinusOneClusters(t *testing.T) {
	var cpus []sysfsfixture.CPU
	for id := 0; id < 8; id++ {
		cpus = append(cpus, sysfsfixture.CPU{
			ID: id, Package: "-1", Core: strconv.Itoa(id % 4), Siblings: strconv.Itoa(id),
		})
	}
	info := logicalProcessors(t, sysfsfixture.Tree{CPUs: cpus})
	want := []string{"0:[0]", "1:[1]", "2:[2]", "3:[3]", "4:[4]", "5:[5]", "6:[6]", "7:[7]"}
	if len(info.Processors) != 1 || info.Processors[0].ID != 0 {
		t.Fatalf("Processors = %v, want one with ID 0", info.Processors)
	}
	if got := coreList(info.Processors[0].Cores); !reflect.DeepEqual(got, want) {
		t.Errorf("cores = %v, want %v", got, want)
	}
}

func TestOfflineLogicalProcessors(t *testing.T) {
	tree := twoSocketTree()
	tree.CPUs = tree.CPUs[:4]
	tree.CPUs[2].Offline, tree.CPUs[3].Offline = true, true
	info := logicalProcessors(t, tree)
	if want := []int{2, 3}; !reflect.DeepEqual(info.OfflineLogicalProcessors, want) {
		t.Errorf("OfflineLogicalProcessors = %v, want %v", info.OfflineLogicalProcessors, want)
	}
	var ids []int
	for _, lp := range info.LogicalProcessors {
		ids = append(ids, lp.ID)
	}
	if want := []int{0, 1}; !reflect.DeepEqual(ids, want) {
		t.Errorf("LogicalProcessors = %v, want %v", ids, want)
	}
}

func TestInfoCaches(t *testing.T) {
	tree := twoSocketTree()
	for i := range tree.CPUs {
		tree.CPUs[i].Caches = []sysfsfixture.Cache{
			{Index: 3, Level: "3", Type: "Unified", ID: strconv.Itoa(i / 4), Size: "16384K"},
		}
	}
	info := logicalProcessors(t, tree)
	if len(info.Caches) != 2 {
		t.Fatalf("Caches = %v, want two L3 instances", info.Caches)
	}
	for p, c := range info.Caches {
		want := []uint32{uint32(p * 4), uint32(p*4 + 1), uint32(p*4 + 2), uint32(p*4 + 3)}
		if c.Level != 3 || c.ID != p || !reflect.DeepEqual(c.LogicalProcessors, want) {
			t.Errorf("Caches[%d] = %+v, want L3 %d of %v", p, c, p, want)
		}
	}
}

// The new fields come in addition to the existing ones, which keep their
// values.
func TestInfoJSONKeepsOldFields(t *testing.T) {
	info := logicalProcessors(t, twoSocketTree())
	var got map[string]any
	if err := json.Unmarshal([]byte(info.JSONString(false)), &got); err != nil {
		t.Fatal(err)
	}
	fields, _ := got["cpu"].(map[string]any)
	for key, want := range map[string]float64{"total_cores": 4, "total_hardware_threads": 8} {
		if fields[key] != want {
			t.Errorf("%s = %v, want %v", key, fields[key], want)
		}
	}
	for _, key := range []string{"processors", "logical_processors"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("JSON has no %q: %v", key, fields)
		}
	}
}

// physical_package_id -1 means "no package information" only when every CPU
// says so; next to CPUs with a real package it is unknown.
func TestPackageIDMixedMinusOne(t *testing.T) {
	tree := twoSocketTree()
	tree.CPUs = tree.CPUs[:4]
	tree.CPUs[0].Package, tree.CPUs[1].Package = "-1", "-1"
	tree.CPUs[2].Package, tree.CPUs[3].Package = "1", "1"
	for _, lp := range logicalProcessors(t, tree).LogicalProcessors {
		want := 1
		if lp.ID < 2 {
			want = -1
		}
		if lp.ProcessorID != want {
			t.Errorf("CPU %d ProcessorID = %d, want %d", lp.ID, lp.ProcessorID, want)
		}
	}
}

// On the x86 snapshots, Processors and their Cores are what they were before
// this package learned about clusters: cores grouped by (package, core_id),
// in the order the CPU directories are listed.
func TestSnapshotProcessorsUnchanged(t *testing.T) {
	dir, err := testdata.SnapshotsDirectory()
	if err != nil {
		t.Fatal(err)
	}
	names, err := filepath.Glob(filepath.Join(dir, "linux-amd64-*.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		t.Run(filepath.Base(name), func(t *testing.T) {
			root := unpackSnapshot(t, filepath.Base(name))
			cpuDir := filepath.Join(root, "sys", "devices", "system", "cpu")
			want := legacyProcessors(t, cpuDir)
			if want == nil {
				t.Skip("no CPU topology in this snapshot")
			}
			info, err := cpu.New(config.WithChroot(root))
			if err != nil {
				t.Fatal(err)
			}
			got := map[int][][2]any{}
			for _, p := range info.Processors {
				for _, c := range p.Cores {
					got[p.ID] = append(got[p.ID], [2]any{c.ID, c.LogicalProcessors})
				}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("processors =\n%v\nwant\n%v", got, want)
			}
		})
	}
}

func coreTypes(t *testing.T, tree sysfsfixture.Tree) []cpu.CoreType {
	t.Helper()
	var types []cpu.CoreType
	for _, lp := range logicalProcessors(t, tree).LogicalProcessors {
		types = append(types, lp.CoreType)
	}
	return types
}

func fourCPUs(capacities ...string) sysfsfixture.Tree {
	tree := twoSocketTree()
	tree.CPUs = tree.CPUs[:4]
	for i, c := range capacities {
		tree.CPUs[i].Capacity = c
	}
	return tree
}

func TestCoreType(t *testing.T) {
	P, E, U, X := cpu.CoreTypePerformance, cpu.CoreTypeEfficiency, cpu.CoreTypeUniform, cpu.CoreTypeUnknown
	hybrid := fourCPUs()
	hybrid.PMUCore, hybrid.PMUAtom = "0-1", "2"
	pmuOnly := fourCPUs()
	pmuOnly.CorePMU = true
	partialPMU := fourCPUs("1024")
	partialPMU.CorePMU = true
	lowPower := fourCPUs()
	lowPower.PMUCore, lowPower.PMULowPower = "0-1", "2-3"
	for name, tc := range map[string]struct {
		tree sysfsfixture.Tree
		want []cpu.CoreType
	}{
		// CPU 3 is in neither list: not guessed.
		"Intel hybrid PMUs":   {hybrid, []cpu.CoreType{P, P, E, X}},
		"Intel low-power":     {lowPower, []cpu.CoreType{P, P, E, E}},
		"big.LITTLE capacity": {fourCPUs("1024", "1024", "446", "446"), []cpu.CoreType{P, P, E, E}},
		"uniform capacity":    {fourCPUs("1024", "1024", "1024", "1024"), []cpu.CoreType{U, U, U, U}},
		"core PMU only":       {pmuOnly, []cpu.CoreType{U, U, U, U}},
		"nothing to tell":     {fourCPUs(), []cpu.CoreType{X, X, X, X}},
		"partial capacity":    {fourCPUs("1024"), []cpu.CoreType{X, X, X, X}},
		// Some CPUs report a capacity: the core PMU does not make them uniform.
		"partial capacity with core PMU": {partialPMU, []cpu.CoreType{X, X, X, X}},
	} {
		t.Run(name, func(t *testing.T) {
			if got := coreTypes(t, tc.tree); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("core types = %v, want %v", got, tc.want)
			}
		})
	}
}

// legacyProcessors groups the online CPUs of a sysfs cpu directory by their
// raw (physical_package_id, core_id), cores in directory order, as ghw did
// before.
func legacyProcessors(t *testing.T, cpuDir string) map[int][][2]any {
	t.Helper()
	entries, err := os.ReadDir(cpuDir)
	if err != nil {
		return nil
	}
	type core struct {
		id  int
		lps []int
	}
	cores := map[int][]*core{}
	for _, e := range entries {
		lp, err := strconv.Atoi(strings.TrimPrefix(e.Name(), "cpu"))
		if err != nil || !strings.HasPrefix(e.Name(), "cpu") {
			continue
		}
		if online, err := os.ReadFile(filepath.Join(cpuDir, e.Name(), "online")); err == nil && strings.TrimSpace(string(online)) == "0" {
			continue
		}
		pkg, err1 := os.ReadFile(filepath.Join(cpuDir, e.Name(), "topology", "physical_package_id"))
		id, err2 := os.ReadFile(filepath.Join(cpuDir, e.Name(), "topology", "core_id"))
		if err1 != nil || err2 != nil {
			return nil
		}
		p, _ := strconv.Atoi(strings.TrimSpace(string(pkg)))
		c, _ := strconv.Atoi(strings.TrimSpace(string(id)))
		var found *core
		for _, existing := range cores[p] {
			if existing.id == c {
				found = existing
			}
		}
		if found == nil {
			found = &core{id: c}
			cores[p] = append(cores[p], found)
		}
		found.lps = append(found.lps, lp)
	}
	if len(cores) == 0 {
		return nil
	}
	out := map[int][][2]any{}
	for p, list := range cores {
		for _, c := range list {
			sort.Ints(c.lps)
			out[p] = append(out[p], [2]any{c.id, c.lps})
		}
	}
	return out
}

func TestCoreTypeJSON(t *testing.T) {
	data, err := json.Marshal(cpu.CoreTypeEfficiency)
	if err != nil || string(data) != `"efficiency"` {
		t.Fatalf("json.Marshal(CoreTypeEfficiency) = %s, %v", data, err)
	}
	var back cpu.CoreType
	if err := json.Unmarshal(data, &back); err != nil || back != cpu.CoreTypeEfficiency {
		t.Errorf("json.Unmarshal(%s) = %v, %v", data, back, err)
	}
}
