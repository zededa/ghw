//
// Use and distribution licensed under the Apache license version 2.
//
// See the COPYING file in the root project directory for full text.
//

package commands

import (
	"fmt"

	"github.com/jaypipes/ghw"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

// canCmd represents the can command
var canCmd = &cobra.Command{
	Use:   "can",
	Short: "Show CAN information for the host system",
	RunE:  showCAN,
}

// showCAN show CAN information for the host system.
func showCAN(cmd *cobra.Command, args []string) error {
	can, err := ghw.CAN()
	if err != nil {
		return errors.Wrap(err, "error getting CAN info")
	}

	switch outputFormat {
	case outputFormatHuman:
		fmt.Printf("%v\n", can)

		for _, device := range can.Devices {
			fmt.Printf(" %v\n", device.Name)
			if device.Parent.PCI != nil {
				fmt.Printf("  PCI: %v\n", device.Parent.PCI)
			}
			if device.Parent.USB != nil {
				fmt.Printf("  USB: %v\n", device.Parent.USB)
			}
		}
	case outputFormatJSON:
		fmt.Printf("%s\n", can.JSONString(pretty))
	case outputFormatYAML:
		fmt.Printf("%s", can.YAMLString())
	}
	return nil
}

func init() {
	rootCmd.AddCommand(canCmd)
}
