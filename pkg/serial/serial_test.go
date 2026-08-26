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

// ttySpec describes one synthetic /sys/class/tty/<name> entry.
type ttySpec struct {
	name string
	// device is the sysfs path, relative to the chroot, that the "device"
	// symlink resolves to. Empty means no symlink at all.
	device string
	attrs  map[string]string
}

type wantDevice struct {
	name    string
	address string
	io      string
	irq     string
	// usbPort, when non-empty, is the expected USB parent port; usbBusnum is
	// only checked alongside it.
	usbBusnum uint16
	usbPort   string
}

func writeTTYTree(t *testing.T, root string, specs []ttySpec) {
	t.Helper()

	ttyDir := filepath.Join(root, "sys", "class", "tty")
	if err := os.MkdirAll(ttyDir, 0755); err != nil {
		t.Fatalf("could not create tty directory: %v", err)
	}

	for _, spec := range specs {
		dir := filepath.Join(ttyDir, spec.name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("could not create %s directory: %v", spec.name, err)
		}
		for attr, value := range spec.attrs {
			path := filepath.Join(dir, attr)
			if err := os.WriteFile(path, []byte(value+"\n"), 0644); err != nil {
				t.Fatalf("could not write %s %s: %v", spec.name, attr, err)
			}
		}
		if spec.device == "" {
			continue
		}
		target := filepath.Join(root, spec.device)
		if err := os.MkdirAll(target, 0755); err != nil {
			t.Fatalf("could not create device dir for %s: %v", spec.name, err)
		}
		if err := os.Symlink(target, filepath.Join(dir, "device")); err != nil {
			t.Fatalf("could not create device symlink for %s: %v", spec.name, err)
		}
	}
}

// TestSerialUnpopulatedPorts covers the sysfs shapes that decide whether a tty
// is a real serial port. Firmware commonly advertises many more 8250 ports than
// the board populates, and the kernel gives every advertised port a tty plus a
// "device" symlink, so those alone must not be taken as proof a port exists.
//
// The attribute sets below are ground truth from a 6.12 kernel. What separates a
// populated port from an advertised-but-absent one is the value of "port", not
// the presence of the attribute: a USB-attached UART is real yet exposes neither
// "port" nor "irq". Note that "type" is 0 for a populated ttyS1 at 0x2f8 just as
// it is for an unpopulated port, so it cannot be used to tell them apart.
func TestSerialUnpopulatedPorts(t *testing.T) {
	populated := func(port, irq, line string) map[string]string {
		return map[string]string{
			"port": port, "irq": irq, "io_type": "0", "type": "0", "line": line,
		}
	}
	// An advertised but unwired 8250 carries the same attributes as a populated
	// one, with the base address and IRQ reading back as zero.
	unpopulated := func(line string) map[string]string {
		return populated("0x0", "0", line)
	}

	const (
		serial8250 = "sys/devices/platform/serial8250/serial8250:0."
		// xHCI controller -> root hub -> port 3 -> interface 0 -> ttyUSB0.
		usbSerialIface = "sys/devices/pci0000:00/0000:00:14.0/usb1/1-3/1-3:1.0/ttyUSB0"
	)

	tests := []struct {
		name string
		ttys []ttySpec
		want []wantDevice
	}{
		{
			name: "populated 8250 is reported",
			ttys: []ttySpec{
				{name: "ttyS0", device: serial8250 + "0", attrs: populated("0x3f8", "4", "0")},
			},
			want: []wantDevice{
				{name: "COM1", address: "/dev/ttyS0", io: "03f8-03ff", irq: "4"},
			},
		},
		{
			name: "unpopulated 8250 is skipped",
			ttys: []ttySpec{
				{name: "ttyS4", device: serial8250 + "4", attrs: unpopulated("4")},
			},
			want: nil,
		},
		{
			name: "USB attached UART is reported despite having no port attribute",
			ttys: []ttySpec{
				{name: "ttyUSB0", device: usbSerialIface},
			},
			want: []wantDevice{
				{
					name: "COM1", address: "/dev/ttyUSB0", io: "", irq: "0",
					usbBusnum: 1, usbPort: "3",
				},
			},
		},
		{
			name: "virtual tty without a device symlink is skipped",
			ttys: []ttySpec{
				{name: "tty0"},
				{name: "ptmx"},
			},
			want: nil,
		},
		{
			// The COM numbering is expected to close up over the skipped
			// phantoms rather than leave gaps.
			name: "phantom ports do not consume COM numbers",
			ttys: []ttySpec{
				{name: "ttyS0", device: serial8250 + "0", attrs: populated("0x3f8", "4", "0")},
				{name: "ttyS1", device: serial8250 + "1", attrs: populated("0x2f8", "3", "1")},
				{name: "ttyS2", device: serial8250 + "2", attrs: unpopulated("2")},
				{name: "ttyS3", device: serial8250 + "3", attrs: unpopulated("3")},
				{name: "ttyS4", device: serial8250 + "4", attrs: unpopulated("4")},
				{name: "ttyS5", device: serial8250 + "5", attrs: populated("0x3e8", "5", "5")},
				{name: "ttyUSB0", device: usbSerialIface},
				{name: "tty0"},
			},
			want: []wantDevice{
				{name: "COM1", address: "/dev/ttyS0", io: "03f8-03ff", irq: "4"},
				{name: "COM2", address: "/dev/ttyS1", io: "02f8-02ff", irq: "3"},
				{name: "COM3", address: "/dev/ttyS5", io: "03e8-03ef", irq: "5"},
				{
					name: "COM4", address: "/dev/ttyUSB0", io: "", irq: "0",
					usbBusnum: 1, usbPort: "3",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeTTYTree(t, root, tt.ttys)

			info, err := New(option.WithChroot(root))
			if err != nil {
				t.Fatalf("New() returned error: %v", err)
			}

			if len(info.Devices) != len(tt.want) {
				t.Fatalf("expected %d devices, got %d: %v",
					len(tt.want), len(info.Devices), info.Devices)
			}

			for i, want := range tt.want {
				got := info.Devices[i]
				if got.Name != want.name {
					t.Errorf("device %d: expected name %s, got %s", i, want.name, got.Name)
				}
				if got.Address != want.address {
					t.Errorf("device %d: expected address %s, got %s", i, want.address, got.Address)
				}
				if got.IO != want.io {
					t.Errorf("device %d (%s): expected IO %q, got %q",
						i, want.address, want.io, got.IO)
				}
				if got.IRQ != want.irq {
					t.Errorf("device %d (%s): expected IRQ %s, got %s",
						i, want.address, want.irq, got.IRQ)
				}
				if want.usbPort == "" {
					continue
				}
				if got.Parent.USB == nil {
					t.Fatalf("device %d (%s): expected a USB parent, got nil",
						i, want.address)
				}
				if got.Parent.USB.Busnum != want.usbBusnum {
					t.Errorf("device %d (%s): expected USB busnum %d, got %d",
						i, want.address, want.usbBusnum, got.Parent.USB.Busnum)
				}
				if got.Parent.USB.Port != want.usbPort {
					t.Errorf("device %d (%s): expected USB port %s, got %s",
						i, want.address, want.usbPort, got.Parent.USB.Port)
				}
			}
		})
	}
}
