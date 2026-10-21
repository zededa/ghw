//
// Use and distribution licensed under the Apache license version 2.
//
// See the COPYING file in the root project directory for full text.
//

package pci

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jaypipes/pcidb"

	"github.com/zededa/ghw/internal/config"
	pciaddr "github.com/zededa/ghw/pkg/pci/address"
)

func TestDeviceLocalCPUs(t *testing.T) {
	root := t.TempDir()
	devices := filepath.Join(root, "sys", "bus", "pci", "devices")
	if err := os.MkdirAll(filepath.Join(devices, "0000:00:1f.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(devices, "0000:00:1f.0", "local_cpulist"), []byte("0-3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(devices, "0000:00:1f.1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(devices, "0000:00:1f.2"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(devices, "0000:00:1f.2", "local_cpulist"), []byte("0-x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := config.ContextFromArgs(config.WithChroot(root))

	if got, want := getDeviceLocalCPUs(ctx, pciaddr.FromString("0000:00:1f.0")), []int{0, 1, 2, 3}; !reflect.DeepEqual(got, want) {
		t.Errorf("local CPUs = %v, want %v", got, want)
	}
	if got := getDeviceLocalCPUs(ctx, pciaddr.FromString("0000:00:1f.1")); got != nil {
		t.Errorf("local CPUs without local_cpulist = %v, want nil", got)
	}
	if got := getDeviceLocalCPUs(ctx, pciaddr.FromString("0000:00:1f.2")); got != nil {
		t.Errorf("local CPUs from a malformed local_cpulist = %v, want nil", got)
	}
}

func TestDeviceLocalCPUsJSON(t *testing.T) {
	dev := &Device{
		Address:              "0000:00:1f.0",
		Vendor:               &pcidb.Vendor{},
		Product:              &pcidb.Product{},
		Subsystem:            &pcidb.Product{},
		Class:                &pcidb.Class{},
		Subclass:             &pcidb.Subclass{},
		ProgrammingInterface: &pcidb.ProgrammingInterface{},
		LocalCPUs:            []int{0, 1},
	}
	data, err := json.Marshal(dev)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"local_cpus":[0,1]`) {
		t.Errorf("JSON %s has no local_cpus", data)
	}
}
