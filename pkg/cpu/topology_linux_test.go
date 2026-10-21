//
// Use and distribution licensed under the Apache license version 2.
//
// See the COPYING file in the root project directory for full text.
//

package cpu_test

import (
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"testing"

	"github.com/zededa/ghw/internal/config"
	"github.com/zededa/ghw/internal/sysfsfixture"
	"github.com/zededa/ghw/pkg/cpu"
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
