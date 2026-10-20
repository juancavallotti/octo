package cmd

import (
	"runtime"

	"github.com/spf13/cobra"
)

func (a *App) versionCommand() *cobra.Command {

	return &cobra.Command{
		Use:     "version",
		Short:   "Print octopaas's version",
		Example: `  octopaas version -o json`,
		Args:    cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {

			info := map[string]string{"version": a.Version, "built": a.BuildDate, "go": runtime.Version(), "platform": runtime.GOOS + "/" + runtime.GOARCH}
			return a.emit(info, func() {
				a.ui.Banner("v" + a.Version)
				a.ui.Print("octopaas %s (built %s, %s, %s)", a.Version, a.BuildDate, info["go"], info["platform"])
			})
		},
	}
}

// environmentTopic is a help page, not a command: `octopaas help environment`.
func environmentTopic() *cobra.Command {

	return &cobra.Command{
		Use:   "environment",
		Short: "The environment variables octopaas reads, and CI/CD use",
		Long: `octopaas reads these environment variables:

  OCTOPAAS_URL      The API to talk to, such as https://api.octopaas.dev.
                    Overrides the profile's url; --url overrides it.
  OCTOPAAS_KEY      An API key (octo_...) to authenticate with. Overrides the
                    profile's key and any signed-in session.
  OCTOPAAS_PROFILE  The profile to use, for one shell or one job, without
                    changing the current one. --profile overrides it.
  OCTOPAAS_HOME     Where config.yaml and credentials.yaml live
                    (default ~/.octopaas).
  NO_COLOR, CI      Either one switches off colours and animations.

Precedence, strongest first: flags, then the environment, then the profile,
then the defaults.

API keys are for automation, such as CI/CD; people should sign in with
` + "`octopaas auth login`" + `, which keeps no secret on disk beyond a renewable
session. In a pipeline, set OCTOPAAS_URL and OCTOPAAS_KEY from your secret
store and run commands directly. No config file is needed, and none is
written:

  export OCTOPAAS_URL=https://api.octopaas.dev
  export OCTOPAAS_KEY=${{ secrets.OCTOPAAS_KEY }}
  octopaas integrations push billing ./billing
  octopaas versions create billing "$GITHUB_REF_NAME"
  octopaas deployments rollout billing-prod --version "$GITHUB_REF_NAME"

A key acts as the user who minted it, with that user's current roles, so mint
it for an account whose roles fit the pipeline.`,
	}
}
