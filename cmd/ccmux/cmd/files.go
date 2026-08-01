package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/skzv/ccmux/internal/config"
)

// newFilesCmd: `ccmux files {list,read}` — the CLI mirror of the TUI's
// Files screen, per the repo's feature-surface policy. Deliberately
// shaped like `ccmux notes` (same subcommand names, same `--host`
// flag, same table-then-raw-body output) so scripting muscle memory
// transfers between the two.
//
// The difference from `notes` is scope: this lists and reads *every*
// file in a project, not just markdown.
func newFilesCmd() *cobra.Command {
	var host string

	parent := &cobra.Command{
		Use:   "files",
		Short: "Browse a project's files on this or another device",
		Long: "List and read any file in a project — not just markdown. With --host, " +
			"operates against a configured remote ccmux device over the tailnet.\n\n" +
			"Version-control, dependency, and build-output directories are pruned, " +
			"matching what the TUI's Files screen shows.",
	}
	parent.PersistentFlags().StringVar(&host, "host", "",
		"configured host name to query (default: this device)")

	list := &cobra.Command{
		Use:   "list <project>",
		Short: "List every file in a project's tree",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			cfg, _ := config.Load()
			// notesClientFor is the shared host→client resolution both
			// subcommand families use; there is nothing notes-specific
			// in it beyond the name.
			cli, _, err := notesClientFor(cfg, host)
			if err != nil {
				return err
			}
			entries, err := cli.Files(ctx, args[0])
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "REL\tSIZE\tMODIFIED")
			for _, e := range entries {
				fmt.Fprintf(tw, "%s\t%d\t%s\n", e.Rel, e.Size, e.Modified.Format("2006-01-02 15:04"))
			}
			return tw.Flush()
		},
	}

	read := &cobra.Command{
		Use:   "read <project> <file>",
		Short: "Print the contents of one file (project-relative path)",
		Long: "Print one file's contents to stdout. Binary files are refused rather " +
			"than printed — dumping them into a terminal corrupts its state. " +
			"Files larger than the server's preview cap are truncated, with a " +
			"note on stderr so a redirected stdout stays clean.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			cfg, _ := config.Load()
			cli, _, err := notesClientFor(cfg, host)
			if err != nil {
				return err
			}
			fc, err := cli.FileContent(ctx, args[0], args[1])
			if err != nil {
				return err
			}
			if fc.Binary {
				return fmt.Errorf("%s is a binary file (%d bytes) — refusing to print it", fc.Rel, fc.Size)
			}
			fmt.Fprint(cmd.OutOrStdout(), fc.Content)
			if fc.Truncated {
				// stderr, so `ccmux files read … > out` gets the bytes
				// and nothing else, and the warning still reaches a
				// human watching the terminal.
				fmt.Fprintf(os.Stderr, "\n[truncated: showing the first %d of %d bytes]\n",
					len(fc.Content), fc.Size)
			}
			return nil
		},
	}

	parent.AddCommand(list, read)
	return parent
}
