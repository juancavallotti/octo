package cmd

import (
	"bytes"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/juancavallotti/octo/octopaas/internal/client"
	"github.com/juancavallotti/octo/octopaas/internal/ui"
)

func (a *App) versionsCommand() *cobra.Command {

	cmd := &cobra.Command{
		Use:     "versions",
		Aliases: []string{"version-tags", "ver"},
		Short:   "Freeze integrations into versions you can deploy",
		Long: `A version freezes an integration as it stands, definition and resources,
under a tag such as v1.3.0. Deployments run versions, never the live draft, so
creating one is how a change becomes deployable. The platform UI calls them
snapshots.

Versions are named by tag within their integration, or by id.`,
		Example: `  octopaas versions create billing v1.3.0
  octopaas versions list billing`,
	}
	cmd.AddCommand(a.versionsListCommand(), a.versionsCreateCommand(), a.versionsRemoveCommand(), a.versionsPullCommand())
	return cmd
}

func (a *App) versionsListCommand() *cobra.Command {

	return &cobra.Command{
		Use:     "list <integration>",
		Aliases: []string{"ls"},
		Short:   "List an integration's versions, newest first",
		Example: `  octopaas versions list billing`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {

			ctx := cmd.Context()
			api, err := a.client()
			if err != nil {
				return err
			}
			var all []client.Version
			err = a.ui.Spin(ui.Tentacles, "Listing versions of "+args[0], func(*ui.Live) error {
				id, err := a.integration(ctx, api, args[0])
				if err != nil {
					return err
				}
				all, err = api.ListVersions(ctx, id)
				return err
			})
			if err != nil {
				return err
			}
			for i := range all {
				all[i].Definition = ""
			}
			return a.emit(all, func() {
				rows := make([][]string, 0, len(all))
				for _, v := range all {
					rows = append(rows, []string{v.Tag, v.ID, ago(v.CreatedAt)})
				}
				a.ui.Table([]string{"TAG", "ID", "CREATED"}, rows)
			})
		},
	}
}

func (a *App) versionsCreateCommand() *cobra.Command {

	return &cobra.Command{
		Use:   "create <integration> <tag>",
		Short: "Freeze an integration under a tag",
		Example: `  octopaas versions create billing v1.3.0
  octopaas versions create billing "$(git describe --tags)"`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {

			ctx := cmd.Context()
			api, err := a.client()
			if err != nil {
				return err
			}
			var v client.Version
			err = a.ui.Spin(ui.Bubbles, "Freezing "+args[0]+" as "+args[1], func(*ui.Live) error {
				id, err := a.integration(ctx, api, args[0])
				if err != nil {
					return err
				}
				v, err = api.CreateVersion(ctx, id, args[1])
				return err
			})
			if err != nil {
				return err
			}
			v.Definition = ""
			return a.emit(v, func() {
				a.ui.Success("Froze %s as %s (%s)", args[0], a.ui.Bold(v.Tag), v.ID)
				a.ui.Hint("Deploy it with `octopaas deployments create %s --version %s`.", args[0], v.Tag)
			})
		},
	}
}

func (a *App) versionsRemoveCommand() *cobra.Command {

	var yes bool
	cmd := &cobra.Command{
		Use:     "rm <integration> <tag>",
		Aliases: []string{"delete"},
		Short:   "Delete a version nothing runs",
		Long:    `Delete a version. A version a deployment is running cannot be deleted; undeploy or roll that deployment onto another version first.`,
		Example: `  octopaas versions rm billing v1.0.0 --yes`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {

			ctx := cmd.Context()
			api, err := a.client()
			if err != nil {
				return err
			}
			versionID, err := a.versionOf(cmd, api, args[0], args[1])
			if err != nil {
				return err
			}
			if err := a.confirm(fmt.Sprintf("Delete version %s of %s?", args[1], args[0]), yes); err != nil {
				return err
			}
			err = a.ui.Spin(ui.Tentacles, "Deleting "+args[1], func(*ui.Live) error {
				return api.DeleteVersion(ctx, versionID)
			})
			if err == nil {
				a.ui.Success("Deleted version %s of %s", args[1], args[0])
			}
			return err
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask first")
	return cmd
}

func (a *App) versionsPullCommand() *cobra.Command {

	var zipFile string
	var force bool
	cmd := &cobra.Command{
		Use:   "pull <integration> <tag> [folder]",
		Short: "Download one version into a folder",
		Long: `Download a version, definition and frozen resources, into a folder (by default
<integration>-<tag>), or with --zip as the bundle it came as.`,
		Example: `  octopaas versions pull billing v1.2.0
  octopaas versions pull billing v1.2.0 --zip billing-v1.2.0.zip`,
		Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {

			ctx := cmd.Context()
			api, err := a.client()
			if err != nil {
				return err
			}
			versionID, err := a.versionOf(cmd, api, args[0], args[1])
			if err != nil {
				return err
			}
			var buf bytes.Buffer
			err = a.ui.Spin(ui.Ink, "Pulling "+args[0]+" "+args[1], func(l *ui.Live) error {
				_, err := api.DownloadVersionBundle(ctx, versionID, &buf, l.Transferred)
				return err
			})
			if err != nil {
				return err
			}
			folder := []string{args[0] + "-" + args[1]}
			if len(args) > 2 {
				folder = append(folder, args[2])
			}
			return a.save(buf.Bytes(), folder, zipFile, force)
		},
	}
	cmd.Flags().StringVar(&zipFile, "zip", "", "save the bundle as this .zip instead of unpacking it")
	cmd.Flags().BoolVar(&force, "force", false, "unpack into a folder that already has files")
	return cmd
}

// versionOf resolves an integration and one of its version tags to the
// version's id.
func (a *App) versionOf(cmd *cobra.Command, api *client.Client, integration, tag string) (string, error) {

	if isID(tag) {
		return tag, nil
	}
	id, err := a.integration(cmd.Context(), api, integration)
	if err != nil {
		return "", err
	}
	return a.version(cmd.Context(), api, id, tag)
}
