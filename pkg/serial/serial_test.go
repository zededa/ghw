//go:build linux
// +build linux

// Use and distribution licensed under the Apache license version 2.
//
// See the COPYING file in the root project directory for full text.
//

package serial

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zededa/ghw/pkg/option"
)

// TestSerialPCIParentBehindBridge verifies that a serial device physically
// located behind a PCI bridge has its Parent.PCI field populated.  Before the
// fix, pciAddress.FromString was called with the full sysfs path, which never
// matches the anchored BDF regex.  The fix calls pci.FindPCIAddress first,
// which walks the path components until it finds the BDF segment.
func TestSerialPCIParentBehindBridge(t *testing.T) {
	root := t.TempDir()

	ttyDir := filepath.Join(root, "sys", "class", "tty")
	if err := os.MkdirAll(ttyDir, 0755); err != nil {
		t.Fatalf("could not create tty directory: %v", err)
	}

	// Simulate: pci0000:00 -> 0000:00:1c.0 (bridge) -> 0000:01:00.0 (UART)
	bridgedDev := filepath.Join(root, "sys", "devices", "pci0000:00",
		"0000:00:1c.0", "0000:01:00.0")
	if err := os.MkdirAll(bridgedDev, 0755); err != nil {
		t.Fatalf("could not create bridged PCI device dir: %v", err)
	}

	ttyS0 := filepath.Join(ttyDir, "ttyS0")
	if err := os.MkdirAll(ttyS0, 0755); err != nil {
		t.Fatalf("could not create ttyS0 directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ttyS0, "irq"), []byte("16\n"), 0644); err != nil {
		t.Fatalf("could not write irq: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ttyS0, "io_type"), []byte("0\n"), 0644); err != nil {
		t.Fatalf("could not write io_type: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ttyS0, "port"), []byte("0xe000\n"), 0644); err != nil {
		t.Fatalf("could not write port: %v", err)
	}
	if err := os.Symlink(bridgedDev, filepath.Join(ttyS0, "device")); err != nil {
		t.Fatalf("could not create device symlink: %v", err)
	}

	info, err := New(option.WithChroot(root))
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	if len(info.Devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(info.Devices))
	}

	dev := info.Devices[0]
	if dev.Parent.PCI == nil {
		t.Fatalf("expected PCI parent to be set for device behind bridge, got nil")
	}
	if dev.Parent.PCI.Domain != "0000" {
		t.Errorf("expected domain 0000, got %s", dev.Parent.PCI.Domain)
	}
	if dev.Parent.PCI.Bus != "01" {
		t.Errorf("expected bus 01, got %s", dev.Parent.PCI.Bus)
	}
	if dev.Parent.PCI.Device != "00" {
		t.Errorf("expected device 00, got %s", dev.Parent.PCI.Device)
	}
	if dev.Parent.PCI.Function != "0" {
		t.Errorf("expected function 0, got %s", dev.Parent.PCI.Function)
	}
}

func TestSerial(t *testing.T) {
	root, err := os.MkdirTemp("", "ghw-serial-test-")
	if err != nil {
		t.Fatalf("could not create temp directory: %v", err)
	}
	defer os.RemoveAll(root)

	// Create /sys/class/tty
	ttyDir := filepath.Join(root, "sys", "class", "tty")
	if err := os.MkdirAll(ttyDir, 0755); err != nil {
		t.Fatalf("could not create tty directory: %v", err)
	}

	// ttyS0 - valid
	ttyS0 := filepath.Join(ttyDir, "ttyS0")
	if err := os.MkdirAll(filepath.Join(ttyS0, "device"), 0755); err != nil {
		t.Fatalf("could not create ttyS0 directory: %v", err)
	}
	// New logic expects irq, io_type, port files
	if err := os.WriteFile(filepath.Join(ttyS0, "irq"), []byte("4\n"), 0644); err != nil {
		t.Fatalf("could not write ttyS0 irq: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ttyS0, "io_type"), []byte("0\n"), 0644); err != nil {
		t.Fatalf("could not write ttyS0 io_type: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ttyS0, "port"), []byte("0x3f8\n"), 0644); err != nil {
		t.Fatalf("could not write ttyS0 port: %v", err)
	}

	// ttyS1 - no device link
	ttyS1 := filepath.Join(ttyDir, "ttyS1")
	if err := os.MkdirAll(ttyS1, 0755); err != nil {
		t.Fatalf("could not create ttyS1 directory: %v", err)
	}

	// ttyS2 - device link but no resources
	ttyS2 := filepath.Join(ttyDir, "ttyS2")
	if err := os.MkdirAll(filepath.Join(ttyS2, "device"), 0755); err != nil {
		t.Fatalf("could not create ttyS2 directory: %v", err)
	}
	// Missing irq/port files, should be skipped or have empty values depending on logic
	// The logic requires portOK && isIOPortUART(ioType) for IO range.
	// And it requires at least one valid property? No, it returns sp, true, nil if device link exists.
	// But serials() filters? No, it appends if ok is true.
	// So ttyS2 will be present but with empty IO/IRQ if files are missing.

	info, err := New(option.WithChroot(root))
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	// ttyS0 and ttyS2 should be found. ttyS1 skipped because no device link.
	if len(info.Devices) != 2 {
		t.Errorf("expected 2 devices, got %d", len(info.Devices))
	}

	if len(info.Devices) > 0 {
		// ttyS0 -> COM1
		dev := info.Devices[0]
		if dev.Name != "COM1" {
			t.Errorf("expected name COM1, got %s", dev.Name)
		}
		if dev.Address != "/dev/ttyS0" {
			t.Errorf("expected address /dev/ttyS0, got %s", dev.Address)
		}
		if dev.IO != "03f8-03ff" {
			t.Errorf("expected IO 03f8-03ff, got %s", dev.IO)
		}
		if dev.IRQ != "4" {
			t.Errorf("expected IRQ 4, got %s", dev.IRQ)
		}
	}

	if len(info.Devices) > 1 {
		// ttyS2 -> COM2
		dev := info.Devices[1]
		if dev.Name != "COM2" {
			t.Errorf("expected name COM2, got %s", dev.Name)
		}
		if dev.Address != "/dev/ttyS2" {
			t.Errorf("expected address /dev/ttyS2, got %s", dev.Address)
		}
		if dev.IO != "" {
			t.Errorf("expected empty IO, got %s", dev.IO)
		}
		if dev.IRQ != "0" {
			t.Errorf("expected IRQ 0, got %s", dev.IRQ)
		}
	}
}
