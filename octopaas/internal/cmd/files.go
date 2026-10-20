package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/juancavallotti/octo/octopaas/internal/client"
	"github.com/juancavallotti/octo/octopaas/internal/ui"
)

func (a *App) filesCommand() *cobra.Command {

	cmd := &cobra.Command{
		Use:     "files",
		Aliases: []string{"file"},
		Short:   "Read and write an integration's config files one at a time",
		Long: `An integration's definition is a set of config files: integration.yaml and whatever
it splits its flows into. These commands work on one file at a time; to work
on all of them, pull and push the integration instead.

A file is named by its path inside the integration, slashes and all.`,
	}
	cmd.AddCommand(a.filesListCommand(), a.filesCatCommand(), a.filesPutCommand(), a.filesRemoveCommand())
	return cmd
}

func (a *App) filesListCommand() *cobra.Command {

	return &cobra.Command{
		Use:     "list <integration>",
		Aliases: []string{"ls"},
		Short:   "List an integration's config files",
		Example: `  octopaas files list billing`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {

			ctx := cmd.Context()
			api, err := a.client()
			if err != nil {
				return err
			}
			var files []client.File
			err = a.ui.Spin(ui.Tentacles, "Listing files in "+args[0], func(*ui.Live) error {
				id, err := a.integration(ctx, api, args[0])
				if err != nil {
					return err
				}
				files, err = api.ListFiles(ctx, id)
				return err
			})
			if err != nil {
				return err
			}
			return a.emit(files, func() {
				rows := make([][]string, 0, len(files))
				for _, f := range files {
					rows = append(rows, []string{f.Path, ago(f.LastUpdated)})
				}
				a.ui.Table([]string{"PATH", "UPDATED"}, rows)
			})
		},
	}
}

func (a *App) filesCatCommand() *cobra.Command {

	return &cobra.Command{
		Use:     "cat <integration> <path>",
		Aliases: []string{"get"},
		Short:   "Print one config file",
		Example: `  octopaas files cat billing integration.yaml
  octopaas files cat billing flows/invoices.yaml > invoices.yaml`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {

			ctx := cmd.Context()
			api, err := a.client()
			if err != nil {
				return err
			}
			var f client.File
			err = a.ui.Spin(ui.Ink, "Reading "+args[1], func(*ui.Live) error {
				id, err := a.integration(ctx, api, args[0])
				if err != nil {
					return err
				}
				f, err = api.GetFile(ctx, id, args[1])
				return err
			})
			if err != nil {
				return err
			}
			if a.output != outputTable {
				return a.emit(f, nil)
			}
			_, err = fmt.Fprint(a.Stdout, f.Content)
			return err
		},
	}
}

func (a *App) filesPutCommand() *cobra.Command {

	var from string
	cmd := &cobra.Command{
		Use:   "put <integration> <path>",
		Short: "Write one config file",
		Long:  `Write one config file, creating it if it is new. The content comes from --from, a file or - for stdin.`,
		Example: `  octopaas files put billing flows/invoices.yaml --from ./invoices.yaml
  echo "flows: []" | octopaas files put billing flows/empty.yaml --from -`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {

			ctx := cmd.Context()
			content, err := a.readInput(from)
			if err != nil {
				return err
			}
			api, err := a.client()
			if err != nil {
				return err
			}
			var f client.File
			err = a.ui.Spin(ui.Bubbles, "Writing "+args[1], func(*ui.Live) error {
				id, err := a.integration(ctx, api, args[0])
				if err != nil {
					return err
				}
				f, err = api.PutFile(ctx, id, args[1], string(content))
				return err
			})
			if err != nil {
				return err
			}
			f.Content = ""
			return a.emit(f, func() { a.ui.Success("Wrote %s in %s", a.ui.Bold(args[1]), args[0]) })
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "the file to upload, or - for stdin")
	_ = cmd.MarkFlagRequired("from")
	return cmd
}

func (a *App) filesRemoveCommand() *cobra.Command {

	var yes bool
	cmd := &cobra.Command{
		Use:     "rm <integration> <path>",
		Aliases: []string{"delete"},
		Short:   "Delete one config file",
		Example: `  octopaas files rm billing flows/old.yaml --yes`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {

			ctx := cmd.Context()
			api, err := a.client()
			if err != nil {
				return err
			}
			id, err := a.integration(ctx, api, args[0])
			if err != nil {
				return err
			}
			if err := a.confirm(fmt.Sprintf("Delete %s from %s?", args[1], args[0]), yes); err != nil {
				return err
			}
			err = a.ui.Spin(ui.Tentacles, "Deleting "+args[1], func(*ui.Live) error {
				return api.DeleteFile(ctx, id, args[1])
			})
			if err == nil {
				a.ui.Success("Deleted %s from %s", args[1], args[0])
			}
			return err
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask first")
	return cmd
}
