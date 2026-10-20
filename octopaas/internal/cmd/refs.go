package cmd

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/juancavallotti/octo/octopaas/internal/client"
)

// Commands take things by name as readily as by id, because names are what
// people remember. An id is passed through untouched; a name is looked up, and
// one that matches nothing or more than one thing is an error that says so.

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func isID(ref string) bool {
	return uuidPattern.MatchString(ref)
}

// integration resolves an integration's name or id to its id.
func (a *App) integration(ctx context.Context, api *client.Client, ref string) (string, error) {

	if isID(ref) {
		return ref, nil
	}
	all, err := api.ListIntegrations(ctx)
	if err != nil {
		return "", err
	}
	var ret []string
	for _, in := range all {
		if in.Name == ref {
			ret = append(ret, in.ID)
		}
	}
	return pick("integration", ref, ret)
}

// deployment resolves a deployment's name or id to its id.
func (a *App) deployment(ctx context.Context, api *client.Client, ref string) (string, error) {

	if isID(ref) {
		return ref, nil
	}
	all, err := api.ListDeployments(ctx)
	if err != nil {
		return "", err
	}
	var ret []string
	for _, d := range all {
		if d.Name == ref {
			ret = append(ret, d.ID)
		}
	}
	return pick("deployment", ref, ret)
}

// version resolves a version's tag or id, within an integration, to its id.
func (a *App) version(ctx context.Context, api *client.Client, integrationID, ref string) (string, error) {

	if isID(ref) {
		return ref, nil
	}
	all, err := api.ListVersions(ctx, integrationID)
	if err != nil {
		return "", err
	}
	var ret []string
	for _, v := range all {
		if v.Tag == ref {
			ret = append(ret, v.ID)
		}
	}
	return pick("version", ref, ret)
}

func pick(kind, ref string, ids []string) (string, error) {

	switch len(ids) {
	case 1:
		return ids[0], nil
	case 0:
		return "", usageError{fmt.Errorf("there is no %s called %q", kind, ref)}
	default:
		return "", usageError{fmt.Errorf("%d %ss are called %q; use an id instead (%s)", len(ids), kind, ref, strings.Join(ids, ", "))}
	}
}
