package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/juancavallotti/octo/octopaas/internal/client"
	"github.com/juancavallotti/octo/octopaas/internal/ui"
)

func (a *App) resourcesCommand() *cobra.Command {

	cmd := &cobra.Command{
		Use:     "resources",
		Aliases: []string{"resource", "res"},
		Short:   "Manage the resources an integration carries",
		Long: `Resources are what an integration carries besides its flows: env files,
templates, prompts and so on. Each has a kind, a name (its path, which may
contain slashes) and content, and is addressed by its id.`,
	}
	cmd.AddCommand(a.resourcesListCommand(), a.resourcesGetCommand(), a.resourcesCreateCommand(),
		a.resourcesUpdateCommand(), a.resourcesRemoveCommand())
	return cmd
}

func (a *App) resourcesListCommand() *cobra.Command {

	return &cobra.Command{
		Use:     "list <integration>",
		Aliases: []string{"ls"},
		Short:   "List an integration's resources",
		Example: `  octopaas resources list billing`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {

			ctx := cmd.Context()
			api, err := a.client()
			if err != nil {
				return err
			}
			var all []client.Resource
			err = a.ui.Spin(ui.Tentacles, "Listing resources in "+args[0], func(*ui.Live) error {
				id, err := a.integration(ctx, api, args[0])
				if err != nil {
					return err
				}
				all, err = api.ListResources(ctx, id)
				return err
			})
			if err != nil {
				return err
			}
			return a.emit(all, func() {
				rows := make([][]string, 0, len(all))
				for _, r := range all {
					rows = append(rows, []string{r.Name, r.Kind, r.ID, ui.Bytes(int64(len(r.Content))), ago(r.LastUpdated)})
				}
				a.ui.Table([]string{"NAME", "KIND", "ID", "SIZE", "UPDATED"}, rows)
			})
		},
	}
}

func (a *App) resourcesGetCommand() *cobra.Command {

	var content bool
	cmd := &cobra.Command{
		Use:   "get <integration> <resource-id>",
		Short: "Show a resource",
		Example: `  octopaas resources get billing 3f1c…
  octopaas resources get billing 3f1c… --content > prompt.md`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {

			ctx := cmd.Context()
			api, err := a.client()
			if err != nil {
				return err
			}
			var r client.Resource
			err = a.ui.Spin(ui.Ink, "Fetching resource "+short(args[1]), func(*ui.Live) error {
				id, err := a.integration(ctx, api, args[0])
				if err != nil {
					return err
				}
				r, err = api.GetResource(ctx, id, args[1])
				return err
			})
			if err != nil {
				return err
			}
			if content {
				_, err := fmt.Fprint(a.Stdout, r.Content)
				return err
			}
			return a.emit(r, func() {
				a.ui.Fields([][2]string{{"Name", r.Name}, {"Kind", r.Kind}, {"Id", r.ID}, {"Size", ui.Bytes(int64(len(r.Content)))}, {"Updated", ago(r.LastUpdated)}})
				a.ui.Hint("Print its content with --content.")
			})
		},
	}
	cmd.Flags().BoolVar(&content, "content", false, "print only the resource's content")
	return cmd
}

// resourceFlags are the flags create and update share.
type resourceFlags struct {
	kind, name, from string
}

func (f *resourceFlags) bind(cmd *cobra.Command) {

	cmd.Flags().StringVar(&f.kind, "kind", "", "what the resource is, such as env or template")
	cmd.Flags().StringVar(&f.name, "name", "", "its path inside the integration (default: the --from file's name)")
	cmd.Flags().StringVar(&f.from, "from", "", "the file holding its content, or - for stdin")
	_ = cmd.MarkFlagRequired("kind")
	_ = cmd.MarkFlagRequired("from")
}

func (a *App) resourceInput(f resourceFlags) (client.ResourceInput, error) {

	content, err := a.readInput(f.from)
	if err != nil {
		return client.ResourceInput{}, err
	}
	name := f.name
	if name == "" && f.from != "-" {
		name = filepath.Base(f.from)
	}
	if name == "" {
		return client.ResourceInput{}, usageError{fmt.Errorf("--name is needed when the content comes from stdin")}
	}
	return client.ResourceInput{Kind: f.kind, Name: name, Content: string(content)}, nil
}

func (a *App) resourcesCreateCommand() *cobra.Command {

	var f resourceFlags
	cmd := &cobra.Command{
		Use:     "create <integration>",
		Short:   "Add a resource",
		Example: `  octopaas resources create billing --kind template --name templates/invoice.html --from invoice.html`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {

			ctx := cmd.Context()
			in, err := a.resourceInput(f)
			if err != nil {
				return err
			}
			api, err := a.client()
			if err != nil {
				return err
			}
			var r client.Resource
			err = a.ui.Spin(ui.Bubbles, "Adding "+in.Name, func(*ui.Live) error {
				id, err := a.integration(ctx, api, args[0])
				if err != nil {
					return err
				}
				r, err = api.CreateResource(ctx, id, in)
				return err
			})
			if err != nil {
				return err
			}
			r.Content = ""
			return a.emit(r, func() { a.ui.Success("Added %s to %s (%s)", a.ui.Bold(r.Name), args[0], r.ID) })
		},
	}
	f.bind(cmd)
	return cmd
}

func (a *App) resourcesUpdateCommand() *cobra.Command {

	var f resourceFlags
	cmd := &cobra.Command{
		Use:     "update <integration> <resource-id>",
		Short:   "Replace a resource",
		Example: `  octopaas resources update billing 3f1c… --kind template --name templates/invoice.html --from invoice.html`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {

			ctx := cmd.Context()
			in, err := a.resourceInput(f)
			if err != nil {
				return err
			}
			api, err := a.client()
			if err != nil {
				return err
			}
			var r client.Resource
			err = a.ui.Spin(ui.Bubbles, "Updating "+in.Name, func(*ui.Live) error {
				id, err := a.integration(ctx, api, args[0])
				if err != nil {
					return err
				}
				r, err = api.UpdateResource(ctx, id, args[1], in)
				return err
			})
			if err != nil {
				return err
			}
			r.Content = ""
			return a.emit(r, func() { a.ui.Success("Updated %s in %s", a.ui.Bold(r.Name), args[0]) })
		},
	}
	f.bind(cmd)
	return cmd
}

func (a *App) resourcesRemoveCommand() *cobra.Command {

	var yes bool
	cmd := &cobra.Command{
		Use:     "rm <integration> <resource-id>",
		Aliases: []string{"delete"},
		Short:   "Delete a resource",
		Example: `  octopaas resources rm billing 3f1c… --yes`,
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
			if err := a.confirm(fmt.Sprintf("Delete resource %s from %s?", short(args[1]), args[0]), yes); err != nil {
				return err
			}
			err = a.ui.Spin(ui.Tentacles, "Deleting resource "+short(args[1]), func(*ui.Live) error {
				return api.DeleteResource(ctx, id, args[1])
			})
			if err == nil {
				a.ui.Success("Deleted resource %s", args[1])
			}
			return err
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask first")
	return cmd
}
