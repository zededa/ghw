//
// Use and distribution licensed under the Apache license version 2.
//
// See the COPYING file in the root project directory for full text.
//

package snapshot_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/zededa/ghw/internal/config"
	"github.com/zededa/ghw/internal/sysfsfixture"
	ghwcpu "github.com/zededa/ghw/pkg/cpu"
	"github.com/zededa/ghw/pkg/snapshot"
)

// NOTE: we intentionally use `os.RemoveAll` - not `snapshot.Cleanup` because we
// want to make sure we never leak directories. `snapshot.Cleanup` is used and
// tested explicitly in `unpack_test.go`.

// nolint: gocyclo
func TestCloneTree(t *testing.T) {
	root, err := snapshot.Unpack(testDataSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)

	cloneRoot, err := os.MkdirTemp("", "ghw-test-clonetree-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(cloneRoot)

	fileSpecs := []string{
		filepath.Join(root, "ghw-test-*"),
		filepath.Join(root, "different/subtree/ghw*"),
		filepath.Join(root, "nested/ghw-test*"),
		filepath.Join(root, "nested/tree/of/subdirectories/forming/deep/unbalanced/tree/ghw-test-3"),
	}
	ctx := context.TODO()
	err = snapshot.CopyFilesInto(ctx, fileSpecs, cloneRoot, nil)
	if err != nil {
		t.Fatal(err)
	}

	origContent, err := scanTree(root, "", []string{""})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(origContent)

	cloneContent, err := scanTree(cloneRoot, cloneRoot, []string{"", "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(cloneContent)

	if len(origContent) != len(cloneContent) {
		t.Fatalf("Expected tree size %d got %d", len(origContent), len(cloneContent))
	}
	if !reflect.DeepEqual(origContent, cloneContent) {
		t.Fatalf("subtree content different expected %#v got %#v", origContent, cloneContent)
	}
}

// nolint: gocyclo
func TestCloneSystemTree(t *testing.T) {
	// ok, this is tricky. Validating a cloned tree is a complex business.
	// We do the bare minimum here to check that both the CloneTree and the ValidateClonedTree did something
	// sensible. To really do a meaningful test we need a more advanced functional test, starting with from
	// a ghw snapshot.
	cloneRoot, err := os.MkdirTemp("", "ghw-test-clonetree-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(cloneRoot)

	ctx := context.TODO()
	err = snapshot.CloneTreeInto(ctx, cloneRoot)
	if err != nil {
		t.Fatal(err)
	}

	cloneContent, err := snapshot.ExpectedCloneContent(ctx)
	if err != nil {
		t.Fatal(err)
	}
	missing, err := snapshot.ValidateClonedTree(cloneContent, cloneRoot)
	if err != nil {
		t.Fatal(err)
	}

	if len(missing) > 0 && areEntriesOnSysfs(missing) {
		t.Fatalf("Expected content %#v missing into the cloned tree %q", missing, cloneRoot)
	}
}

func areEntriesOnSysfs(sysfsEntries []string) bool {
	// turns out some ISA bridges do not actually expose the driver entry. The reason is not clear.
	// So let's check if we actually have the entry we were looking for on sysfs. If so, we
	// actually failed to clone an entry, and we must fail the test. Otherwise we carry on.
	for _, sysfsEntry := range sysfsEntries {
		if _, err := os.Lstat(sysfsEntry); err == nil {
			return true
		}
	}
	return false
}

func scanTree(root, prefix string, excludeList []string) ([]string, error) {
	var contents []string
	return contents, filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fp := strings.TrimPrefix(path, prefix); !includedInto(fp, excludeList) {
			contents = append(contents, fp)
		}
		return nil
	})
}

func includedInto(s string, items []string) bool {
	if items == nil {
		return false
	}
	for _, item := range items {
		if s == item {
			return true
		}
	}
	return false
}

// A snapshot reproduces the CPU topology ghw reads from the host: online and
// offline CPUs, packages, cores, NUMA nodes, frequencies, caches and core types.
func TestCloneStaticContentCPUTopology(t *testing.T) {
	cpu := func(id int, core string, siblings string) sysfsfixture.CPU {
		return sysfsfixture.CPU{
			ID: id, Package: "0", Core: core, Siblings: siblings,
			BaseKHz: "2100000", MaxKHz: "4800000",
			Caches: []sysfsfixture.Cache{
				{Index: 0, Level: "1", Type: "Data", ID: core, Size: "48K", Shared: siblings},
				{Index: 2, Level: "2", Type: "Unified", ID: "0", Size: "2048K", Shared: "0-3"},
			},
		}
	}
	hybrid := sysfsfixture.Tree{
		CPUs: []sysfsfixture.CPU{
			cpu(0, "0", "0-1"), cpu(1, "0", "0-1"), cpu(2, "8", "2"), cpu(3, "9", "3"),
			{ID: 4, Offline: true},
		},
		Nodes:   map[int]string{0: "0-3"},
		PMUCore: "0-1", PMUAtom: "2", PMULowPower: "3",
	}
	bigLittle := sysfsfixture.Tree{Nodes: map[int]string{0: "0-3"}}
	for id, capacity := range []string{"1024", "1024", "446", "446"} {
		c := cpu(id, strconv.Itoa(id), strconv.Itoa(id))
		c.Capacity = capacity
		bigLittle.CPUs = append(bigLittle.CPUs, c)
	}
	homogeneous := sysfsfixture.Tree{
		CPUs:    []sysfsfixture.CPU{cpu(0, "0", "0-1"), cpu(1, "0", "0-1")},
		CorePMU: true,
	}
	for name, tree := range map[string]sysfsfixture.Tree{
		"Intel hybrid":    hybrid,
		"big.LITTLE":      bigLittle,
		"homogeneous x86": homogeneous,
	} {
		t.Run(name, func(t *testing.T) {
			host := sysfsfixture.Write(t, tree)
			var specs []string
			for _, spec := range snapshot.ExpectedCloneStaticContent() {
				specs = append(specs, filepath.Join(host, spec))
			}
			clone := t.TempDir()
			if err := snapshot.CopyFilesInto(context.Background(), specs, clone, nil); err != nil {
				t.Fatal(err)
			}
			want, err := ghwcpu.New(config.WithChroot(host))
			if err != nil {
				t.Fatal(err)
			}
			got, err := ghwcpu.New(config.WithChroot(filepath.Join(clone, host)))
			if err != nil {
				t.Fatal(err)
			}
			if got.JSONString(true) != want.JSONString(true) {
				t.Errorf("from the snapshot:\n%s\nfrom the host:\n%s", got.JSONString(true), want.JSONString(true))
			}
		})
	}
}
