package cmd

import (
	"log"

	"github.com/hupe1980/log4shellscan/internal"
	"github.com/spf13/cobra"
)

type localOptions struct {
	ignoreExts []string
	ignoreV1   bool
}

func newLocalCmd(verbose *bool) *cobra.Command {
	opts := &localOptions{}

	cmd := &cobra.Command{
		Use:           "local [paths]",
		Short:         "Scan for vulnerable log4j versions",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			log.Printf("[i] Log4Shell CVE-2021-44228 Local Vulnerability Scan")

			internal.FilePathWalk(&internal.LocalOptions{
				Roots:      args,
				IgnoreExts: opts.ignoreExts,
				Verbose:    *verbose,
			})

			log.Printf("[i] Completed scanning")

			return nil
		},
	}

	cmd.Flags().BoolVarP(&opts.ignoreV1, "ignore-v1", "", false, "ignore log4j 1.x versions")
	cmd.Flags().StringArrayVarP(&opts.ignoreExts, "ignore-ext", "", []string{}, "ignore .jar | .zip | .war | .ear | .aar")

	return cmd
}
