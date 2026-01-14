package commands

import (
	"fmt"

	"github.com/zededa/ghw"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

// tpmCmd represents the tpm command
var tpmCmd = &cobra.Command{
	Use:   "tpm",
	Short: "Show TPM information for the host system",
	RunE:  showTPM,
}

// showTPM show TPM information for the host system.
func showTPM(cmd *cobra.Command, args []string) error {
	tpm, err := ghw.TPM()
	if err != nil {
		return errors.Wrap(err, "error getting TPM info")
	}

	switch outputFormat {
	case outputFormatHuman:
		fmt.Printf("%v\n", tpm)
	case outputFormatJSON:
		fmt.Printf("%s\n", tpm.JSONString(pretty))
	case outputFormatYAML:
		fmt.Printf("%s", tpm.YAMLString())
	}
	return nil
}

func init() {
	rootCmd.AddCommand(tpmCmd)
}
