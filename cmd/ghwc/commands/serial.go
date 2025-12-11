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

// serialCmd represents the `serial` command
var serialCmd = &cobra.Command{
	Use:   "serial",
	Short: "Show serial information for the host system",
	RunE:  showSerial,
}

// showSerial show serial information for the host system.
func showSerial(cmd *cobra.Command, args []string) error {
	serial, err := ghw.Serial()
	if err != nil {
		return errors.Wrap(err, "error getting serial info")
	}

	switch outputFormat {
	case outputFormatHuman:
		fmt.Printf("%v\n", serial)
		for _, device := range serial.Devices {
			fmt.Printf(" %+v\n", device)
		}
	case outputFormatJSON:
		fmt.Printf("%s\n", serial.JSONString(pretty))
	case outputFormatYAML:
		fmt.Printf("%s", serial.YAMLString())
	}
	return nil
}

func init() {
	rootCmd.AddCommand(serialCmd)
}
