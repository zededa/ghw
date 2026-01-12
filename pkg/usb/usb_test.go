//go:build linux
// +build linux

//
// Use and distribution licensed under the Apache license version 2.
//
// See the COPYING file in the root project directory for full text.
//

package usb

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jaypipes/ghw/pkg/bus"
	"github.com/jaypipes/ghw/pkg/option"
	pciAddress "github.com/jaypipes/ghw/pkg/pci/address"
	"github.com/jaypipes/ghw/pkg/snapshot"
	usbAddress "github.com/jaypipes/ghw/pkg/usb/address"
	"github.com/jaypipes/ghw/testdata"
)

func TestUSB(t *testing.T) {
	usbDir, err := os.MkdirTemp("", "TestUSB")
	if err != nil {
		t.Fatalf("could not create temp directory: %v", err)
	}

	data := `
DEVTYPE=usb_interface
DRIVER=usbhid
PRODUCT=46a/a087/101
TYPE=0/0/0
INTERFACE=3/1/2
MODALIAS=usb:v046ApA087d0101dc00dsc00dp00ic03isc01ip02in00
	`

	err = os.WriteFile(filepath.Join(usbDir, "uevent"), []byte(data), 0600)
	if err != nil {
		t.Fatalf("could not write uevent file in %s: %+v", usbDir, err)
	}

	var d Device
	err = fillUSBFromUevent(usbDir, &d)
	if err != nil {
		t.Fatalf("could not fill USB info from uevent file: %v", err)
	}

	deviceExpected := Device{
		Driver:     "usbhid",
		Type:       "0/0/0",
		VendorID:   "46a",
		ProductID:  "a087",
		RevisionID: "101",
	}

	if !reflect.DeepEqual(d, deviceExpected) {
		t.Fatalf("expected: %+v, but got %+v", deviceExpected, d)
	}

}

func TestSnapshot(t *testing.T) {
	testdataPath, err := testdata.SnapshotsDirectory()
	if err != nil {
		t.Fatalf("Expected nil err, but got %v", err)
	}

	snapshotPath := filepath.Join(testdataPath, "linux-amd64-intel-i7-1270P.tar.gz")
	unpackDir := t.TempDir()
	err = snapshot.UnpackInto(snapshotPath, unpackDir)
	if err != nil {
		t.Fatal(err)
	}

	info, err := New(option.WithChroot(unpackDir))
	if err != nil {
		t.Fatalf("Expected nil err, but got %v", err)
	}
	if info == nil {
		t.Fatalf("Expected non-nil USB info, but got nil")
	}

	usbParentConsistencyCheck(t, info.Devices)
}
func TestDeviceParent(t *testing.T) {
	info, err := New()
	if err != nil {
		t.Fatal(err)
	}
	devs := info.Devices

	usbParentConsistencyCheck(t, devs)
}

func usbParentConsistencyCheck(t *testing.T, devs []*Device) {
	addressesSeen := map[usbAddress.Address]struct{}{}
	for _, dev := range devs {
		addressesSeen[dev.Address] = struct{}{}
	}
	for _, dev := range devs {
		if dev.Parent.USB == nil {
			continue
		}
		_, found := addressesSeen[*dev.Parent.USB]
		if !found {
			t.Fatalf("could not find parent device for %v, addr: %v\n", dev, dev.Parent.USB)
		}
	}
}
func TestDeviceString(t *testing.T) {
	tests := []struct {
		name     string
		device   Device
		contains []string
	}{
		{
			name: "Basic device",
			device: Device{
				VendorID:  "8086",
				ProductID: "1234",
				Devnum:    "002",
				Address: usbAddress.Address{
					Busnum: 1,
					Port:   "2",
				},
			},
			contains: []string{"vendorID=8086", "productID=1234", "address=1-2"},
		},
		{
			name: "Device with USB parent",
			device: Device{
				VendorID:  "8086",
				ProductID: "1234",
				Devnum:    "003",
				Parent: bus.BusParent{
					USB: &usbAddress.Address{
						Busnum: 001,
						Port:   "001",
					},
				},
				Address: usbAddress.Address{
					Busnum: 2,
					Port:   "3",
				},
			},
			contains: []string{"address=2-3", "parent-usb=1-001"},
		},
		{
			name: "Device with PCI parent",
			device: Device{
				VendorID:  "8086",
				ProductID: "1234",
				Devnum:    "003",
				Parent: bus.BusParent{
					PCI: &pciAddress.Address{
						Domain:   "0000",
						Bus:      "00",
						Device:   "1f",
						Function: "6",
					},
				},
				Address: usbAddress.Address{
					Busnum: 2,
					Port:   "1.2",
				},
			},
			contains: []string{"address=2-1.2", "parent-pci=0000:00:1f.6"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.device.String()
			for _, substr := range tc.contains {
				if !strings.Contains(s, substr) {
					t.Errorf("String() = %q; want it to contain %q", s, substr)
				}
			}
		})
	}
}
