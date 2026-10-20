package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/juancavallotti/octo/octopaas/internal/client"
	"github.com/juancavallotti/octo/octopaas/internal/ui"
)

func (a *App) integrationsCommand() *cobra.Command {

	cmd := &cobra.Command{
		Use:     "integrations",
		Aliases: []string{"integration", "int"},
		Short:   "Create, edit, pull and push integrations",
		Long: `Integrations are the flows you build. Every command takes an integration by
name or by id.

The quickest way to edit one is to pull it into a folder, change the files with
any editor, and push it back: it keeps its id, its versions and its
deployments.`,
		Example: `  octopaas integrations list
  octopaas integrations pull billing
  $EDITOR billing/integration.yaml
  octopaas integrations push billing ./billing`,
	}
	cmd.AddCommand(
		a.integrationsListCommand(), a.integrationsGetCommand(), a.integrationsCreateCommand(),
		a.integrationsRenameCommand(), a.integrationsRemoveCommand(),
		a.integrationsPullCommand(), a.integrationsPushCommand(), a.integrationsImportCommand(),
	)
	return cmd
}

func (a *App) integrationsListCommand() *cobra.Command {

	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List integrations",
		Example: `  octopaas integrations list
  octopaas int ls -o json | jq -r '.[].name'`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {

			api, err := a.client()
			if err != nil {
				return err
			}
			var all []client.Integration
			err = a.ui.Spin(ui.Tentacles, "Gathering integrations", func(*ui.Live) error {
				all, err = api.ListIntegrations(cmd.Context())
				return err
			})
			if err != nil {
				return err
			}
			//nobody listing integrations wants every YAML in the output.
			for i := range all {
				all[i].Definition = ""
			}
			return a.emit(all, func() {
				rows := make([][]string, 0, len(all))
				for _, in := range all {
					by := in.UpdatedByName
					if by == "" {
						by = in.UpdatedByEmail
					}
					rows = append(rows, []string{in.Name, in.ID, ago(in.LastUpdated), by})
				}
				a.ui.Table([]string{"NAME", "ID", "UPDATED", "BY"}, rows)
			})
		},
	}
}

func (a *App) integrationsGetCommand() *cobra.Command {

	var definition bool
	cmd := &cobra.Command{
		Use:   "get <integration>",
		Short: "Show an integration",
		Long:  `Show an integration, or with --definition print just its YAML, ready to redirect into a file.`,
		Example: `  octopaas integrations get billing
  octopaas integrations get billing --definition > octo.yaml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {

			ctx := cmd.Context()
			api, err := a.client()
			if err != nil {
				return err
			}
			var in client.Integration
			err = a.ui.Spin(ui.Tentacles, "Fetching "+args[0], func(*ui.Live) error {
				id, err := a.integration(ctx, api, args[0])
				if err != nil {
					return err
				}
				in, err = api.GetIntegration(ctx, id)
				return err
			})
			if err != nil {
				return err
			}
			if definition {
				_, err := fmt.Fprint(a.Stdout, in.Definition)
				return err
			}
			return a.emit(in, func() {
				a.ui.Fields([][2]string{
					{"Name", in.Name},
					{"Id", in.ID},
					{"Updated", fmt.Sprintf("%s by %s", ago(in.LastUpdated), firstOf(in.UpdatedByName, in.UpdatedByEmail))},
					{"Created by", firstOf(in.CreatedByName, in.CreatedByEmail)},
				})
				a.ui.Hint("Print its YAML with --definition, or pull the whole thing with `octopaas integrations pull %s`.", in.Name)
			})
		},
	}
	cmd.Flags().BoolVar(&definition, "definition", false, "print only the integration's YAML")
	return cmd
}

func (a *App) integrationsCreateCommand() *cobra.Command {

	var file string
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create an integration from a YAML file",
		Long: `Create an integration from its YAML definition. To create one with resources
as well, import a bundle instead.`,
		Example: `  octopaas integrations create billing --file octo.yaml
  cat octo.yaml | octopaas integrations create billing --file -`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {

			definition, err := a.readInput(file)
			if err != nil {
				return err
			}
			api, err := a.client()
			if err != nil {
				return err
			}
			var in client.Integration
			err = a.ui.Spin(ui.Bubbles, "Creating "+args[0], func(*ui.Live) error {
				in, err = api.CreateIntegration(cmd.Context(), args[0], string(definition))
				return err
			})
			if err != nil {
				return err
			}
			return a.emit(in, func() { a.ui.Success("Created %s (%s)", a.ui.Bold(in.Name), in.ID) })
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "the YAML definition, or - for stdin")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func (a *App) integrationsRenameCommand() *cobra.Command {

	return &cobra.Command{
		Use:     "rename <integration> <new-name>",
		Short:   "Rename an integration",
		Example: `  octopaas integrations rename billing invoicing`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {

			ctx := cmd.Context()
			api, err := a.client()
			if err != nil {
				return err
			}
			var in client.Integration
			err = a.ui.Spin(ui.Tentacles, "Renaming "+args[0]+" to "+args[1], func(*ui.Live) error {
				id, err := a.integration(ctx, api, args[0])
				if err != nil {
					return err
				}
				in, err = api.RenameIntegration(ctx, id, args[1])
				return err
			})
			if err != nil {
				return err
			}
			in.Definition = ""
			return a.emit(in, func() { a.ui.Success("Renamed %s to %s", args[0], a.ui.Bold(in.Name)) })
		},
	}
}

func (a *App) integrationsRemoveCommand() *cobra.Command {

	var yes bool
	cmd := &cobra.Command{
		Use:     "rm <integration>",
		Aliases: []string{"delete"},
		Short:   "Delete an integration",
		Long:    `Delete an integration, with its files, resources and versions. There is no undo.`,
		Example: `  octopaas integrations rm scratchpad
  octopaas integrations rm scratchpad --yes`,
		Args: cobra.ExactArgs(1),
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
			if err := a.confirm(fmt.Sprintf("Delete integration %s for good?", args[0]), yes); err != nil {
				return err
			}
			err = a.ui.Spin(ui.Tentacles, "Deleting "+args[0], func(*ui.Live) error {
				return api.DeleteIntegration(ctx, id)
			})
			if err == nil {
				a.ui.Success("Deleted %s", args[0])
			}
			return err
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask first")
	return cmd
}

func (a *App) integrationsPullCommand() *cobra.Command {

	var zipFile string
	var force bool
	cmd := &cobra.Command{
		Use:   "pull <integration> [folder]",
		Short: "Download an integration into a folder",
		Long: `Download an integration, definition and resources, and unpack it into a folder
(by default one named after it). The folder must be empty unless you add
--force, which overwrites the files the bundle carries and leaves the rest.

With --zip, the bundle is saved as it came instead.`,
		Example: `  octopaas integrations pull billing
  octopaas integrations pull billing ./work/billing --force
  octopaas integrations pull billing --zip billing.zip`,
		Args: cobra.RangeArgs(1, 2),
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

			var buf bytes.Buffer
			err = a.ui.Spin(ui.Ink, "Pulling "+args[0], func(l *ui.Live) error {
				_, err := api.DownloadBundle(ctx, id, &buf, l.Transferred)
				return err
			})
			if err != nil {
				return err
			}
			return a.save(buf.Bytes(), args, zipFile, force)
		},
	}
	cmd.Flags().StringVar(&zipFile, "zip", "", "save the bundle as this .zip instead of unpacking it")
	cmd.Flags().BoolVar(&force, "force", false, "unpack into a folder that already has files")
	return cmd
}

// save writes a pulled bundle where the person asked: a zip, or a folder named
// by the second argument or after the first.
func (a *App) save(data []byte, args []string, zipFile string, force bool) error {

	if zipFile != "" {
		if err := os.WriteFile(zipFile, data, 0o644); err != nil {
			return err
		}
		a.ui.Success("Saved %s (%s)", zipFile, ui.Bytes(int64(len(data))))
		return nil
	}

	dir := filepath.Base(args[0])
	if len(args) > 1 {
		dir = args[1]
	}
	if !force && !emptyOrMissing(dir) {
		return usageError{fmt.Errorf("%s already has files in it; add --force to unpack over them, or pick another folder", dir)}
	}
	n, err := unpack(data, dir)
	if err != nil {
		return err
	}
	a.ui.Box("Pulled "+args[0], fmt.Sprintf("%d files into %s/", n, dir), "", "Push it back when you are done:", "  octopaas integrations push "+args[0]+" "+dir)
	return nil
}

func (a *App) integrationsPushCommand() *cobra.Command {

	cmd := &cobra.Command{
		Use:   "push <integration> [folder | bundle.zip]",
		Short: "Replace an integration with a folder or a zip",
		Long: `Pack a folder (by default the one named after the integration) and replace the
integration's contents with it. It keeps its id, name, versions and
deployments; running deployments are untouched until you roll them out.

Hidden files and folders, such as .git and .env, are never pushed.`,
		Example: `  octopaas integrations push billing
  octopaas integrations push billing ./work/billing
  octopaas integrations push billing billing.zip`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {

			ctx := cmd.Context()
			source := filepath.Base(args[0])
			if len(args) > 1 {
				source = args[1]
			}
			data, files, err := readBundle(source)
			if err != nil {
				return err
			}
			api, err := a.client()
			if err != nil {
				return err
			}
			id, err := a.integration(ctx, api, args[0])
			if err != nil {
				return err
			}

			var in client.Integration
			err = a.ui.Spin(ui.Bubbles, "Pushing "+source, func(l *ui.Live) error {
				in, err = api.ReplaceBundle(ctx, id, data, l.Transferred)
				return err
			})
			if err != nil {
				return err
			}
			in.Definition = ""
			return a.emit(in, func() {
				a.ui.Success("%s now holds %d files (%s)", a.ui.Bold(in.Name), files, ui.Bytes(int64(len(data))))
				a.ui.Hint("Freeze it with `octopaas versions create %s <tag>` to deploy it.", in.Name)
			})
		},
	}
	return cmd
}

func (a *App) integrationsImportCommand() *cobra.Command {

	var name string
	cmd := &cobra.Command{
		Use:   "import <folder | bundle.zip>",
		Short: "Create an integration from a folder or a zip",
		Long: `Create a new integration from a folder or a bundle. Its name is the one the
bundle carries, or --name when it carries none; a taken name gets a numbered
suffix.`,
		Example: `  octopaas integrations import ./billing --name billing
  octopaas integrations import billing.zip`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {

			data, files, err := readBundle(args[0])
			if err != nil {
				return err
			}
			if name == "" {
				name = filepath.Base(args[0])
			}
			api, err := a.client()
			if err != nil {
				return err
			}
			var in client.Integration
			err = a.ui.Spin(ui.Bubbles, "Importing "+args[0], func(l *ui.Live) error {
				in, err = api.ImportBundle(cmd.Context(), name, data, l.Transferred)
				return err
			})
			if err != nil {
				return err
			}
			in.Definition = ""
			return a.emit(in, func() {
				a.ui.Success("Imported %s (%s) with %d files", a.ui.Bold(in.Name), in.ID, files)
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "the name to use when the bundle carries none (default: the folder's)")
	return cmd
}

func firstOf(values ...string) string {

	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return "—"
}
