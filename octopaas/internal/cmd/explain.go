package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"

	"github.com/juancavallotti/octo/octopaas/internal/client"
	"github.com/juancavallotti/octo/octopaas/internal/config"
	"github.com/juancavallotti/octo/octopaas/internal/oauth"
	"github.com/juancavallotti/octo/octopaas/internal/ui"
)

// explain tells the person what went wrong and what to do about it. Every
// failure a command can meet ends here, so each gets one sentence about what
// happened and one about what to try, rather than a Go error chain.
func (a *App) explain(err error) {

	if errors.Is(err, errCancelled) {
		a.ui.Say("Left as it was.")
		return
	}
	var status apiStatus
	if errors.As(err, &status) {
		a.ui.Say("%s", status.Error())
		return
	}

	var usage usageError
	if errors.As(err, &usage) {
		a.ui.Moment(ui.Confused, usage.Error(), "Add --help to the command to see how it is used.")
		return
	}

	if errors.Is(err, oauth.ErrSignedOut) {
		a.ui.Moment(ui.Sleepy, fmt.Sprintf("%s (profile %s).", capitalize(err.Error()), a.resolved.Profile),
			"Sign in with `octopaas auth login`, or set OCTOPAAS_KEY to use an API key.")
		return
	}

	//context.DeadlineExceeded also passes for a net.Error, so a wait that ran
	//out is told apart before anything is blamed on the network.
	if errors.Is(err, context.DeadlineExceeded) {
		a.ui.Moment(ui.Sleepy, capitalize(err.Error())+".", "Nothing was changed by the wait running out; run the command again when you are ready.")
		return
	}

	var urlErr *url.Error
	var netErr net.Error
	if errors.As(err, &urlErr) || errors.As(err, &netErr) {
		a.ui.Moment(ui.Lost, fmt.Sprintf("Could not reach %s.", a.resolved.URL),
			"Check the address (--url, OCTOPAAS_URL or the profile's url) and your connection. "+err.Error())
		return
	}

	var apiErr *client.Error
	if !errors.As(err, &apiErr) {
		a.ui.Moment(ui.Sad, capitalize(err.Error()), "")
		return
	}

	message := capitalize(apiErr.Message)
	switch {
	case apiErr.Status == http.StatusUnauthorized:
		a.ui.Moment(ui.Sleepy, message, a.credentialHint())
	case apiErr.Status == http.StatusForbidden:
		a.ui.Moment(ui.Confused, message,
			"The platform knows who you are, but this needs a role you do not have. Ask an administrator; `octopaas whoami` shows your roles.")
	case apiErr.Status == http.StatusNotFound:
		a.ui.Moment(ui.Searching, message, "Check the id or name. List what there is with the matching `list` command.")
	case apiErr.Status == http.StatusConflict:
		a.ui.Moment(ui.Confused, message, "Something already there is in the way: pick another name or tag, or remove what is using it first.")
	case apiErr.Status == http.StatusRequestEntityTooLarge:
		a.ui.Moment(ui.Confused, "That bundle is larger than the API takes (8 MiB).", "Leave large or generated files out of the folder you push.")
	case apiErr.Status >= 500:
		a.ui.Moment(ui.Sad, message, "The platform could not do this right now; your credential is fine. Try again in a moment.")
	default:
		a.ui.Moment(ui.Confused, message, "")
	}
}

// credentialHint is what to do about a refused credential, depending on where
// it came from.
func (a *App) credentialHint() string {

	switch {
	case a.resolved.KeySource == config.FromEnv:
		return "OCTOPAAS_KEY was refused: it may be revoked or expired, or belong to another platform. Mint a new one on your account page."
	case a.resolved.APIKey != "":
		return fmt.Sprintf("The API key in profile %s was refused. Replace it with `octopaas profile add %s --with-key`.", a.resolved.Profile, a.resolved.Profile)
	default:
		return "Your session was refused. Sign in again with `octopaas auth login`."
	}
}

func capitalize(s string) string {

	if s == "" {
		return s
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-'a'+'A') + s[1:]
	}
	return s
}
