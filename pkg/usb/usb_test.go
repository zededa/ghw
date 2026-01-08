//go:build linux
// +build linux

//
// Use and distribution licensed under the Apache license version 2.
//
// See the COPYING file in the root project directory for full text.
//

package usb

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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

func TestDeviceParent(t *testing.T) {
	info, err := New()
	if err != nil {
		t.Fatal(err)
	}

	addressesSeen := map[string]struct{}{}
	for _, dev := range info.Devices {
		addr := fmt.Sprintf("%d-%s", dev.Busnum, dev.Port)
		addressesSeen[addr] = struct{}{}
	}
	for _, dev := range info.Devices {
		if dev.Parent.USB == nil {
			continue
		}
		parentAddr := fmt.Sprintf("%d-%s", dev.Parent.USB.Busnum, dev.Parent.USB.Port)
		_, found := addressesSeen[parentAddr]
		if !found {
			t.Fatalf("could not find parent device for %s, addr: %s\n", dev, parentAddr)
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
				USBAddress: USBAddress{
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
				Parent: BusParent{
					USB: &USBAddress{
						Busnum: 001,
						Port:   "001",
					},
				},
				USBAddress: USBAddress{
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
				Parent: BusParent{
					PCI: &PCIAddress{
						Domain:   "0000",
						Bus:      "00",
						Device:   "1f",
						Function: "6",
					},
				},
				USBAddress: USBAddress{
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
