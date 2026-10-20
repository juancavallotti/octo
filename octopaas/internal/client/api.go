package client

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// One method per operation, in the order the spec lists them.

// Me is the account the credential acts as.
func (c *Client) Me(ctx context.Context) (User, error) {

	var ret User
	err := c.call(ctx, request{route: getMe}, &ret)
	return ret, err
}

// ListIntegrations lists every integration.
func (c *Client) ListIntegrations(ctx context.Context) ([]Integration, error) {

	var ret []Integration
	err := c.call(ctx, request{route: listIntegrations}, &ret)
	return ret, err
}

// CreateIntegration creates an integration from its YAML.
func (c *Client) CreateIntegration(ctx context.Context, name, definition string) (Integration, error) {

	var ret Integration
	r, err := jsonBody(request{route: createIntegration}, map[string]string{"name": name, "definition": definition})
	if err != nil {
		return ret, err
	}
	err = c.call(ctx, r, &ret)
	return ret, err
}

// GetIntegration reads one integration, its YAML included.
func (c *Client) GetIntegration(ctx context.Context, id string) (Integration, error) {

	var ret Integration
	err := c.call(ctx, request{route: getIntegration, path: []string{id}}, &ret)
	return ret, err
}

// RenameIntegration gives an integration a new name.
func (c *Client) RenameIntegration(ctx context.Context, id, name string) (Integration, error) {

	var ret Integration
	r, err := jsonBody(request{route: renameIntegration, path: []string{id}}, map[string]string{"name": name})
	if err != nil {
		return ret, err
	}
	err = c.call(ctx, r, &ret)
	return ret, err
}

// DeleteIntegration deletes an integration.
func (c *Client) DeleteIntegration(ctx context.Context, id string) error {
	return c.call(ctx, request{route: deleteIntegration, path: []string{id}}, nil)
}

// DownloadBundle writes an integration's zip into w.
func (c *Client) DownloadBundle(ctx context.Context, id string, w io.Writer, progress Progress) (int64, error) {
	return c.download(ctx, request{route: downloadBundle, path: []string{id}}, w, progress)
}

// ReplaceBundle replaces an integration's contents with a zip.
func (c *Client) ReplaceBundle(ctx context.Context, id string, zip []byte, progress Progress) (Integration, error) {

	var ret Integration
	r := request{route: replaceBundle, path: []string{id}, body: zip, contentType: "application/zip", upload: progress}
	err := c.call(ctx, r, &ret)
	return ret, err
}

// ImportBundle creates an integration from a zip. name is used when the bundle
// carries none.
func (c *Client) ImportBundle(ctx context.Context, name string, zip []byte, progress Progress) (Integration, error) {

	var ret Integration
	r := request{route: importBundle, body: zip, contentType: "application/zip", upload: progress}
	if name != "" {
		r.query = url.Values{"name": {name}}
	}
	err := c.call(ctx, r, &ret)
	return ret, err
}

// ListFiles lists an integration's config files, without their contents.
func (c *Client) ListFiles(ctx context.Context, id string) ([]File, error) {

	var ret []File
	err := c.call(ctx, request{route: listFiles, path: []string{id}}, &ret)
	return ret, err
}

// ReplaceFiles replaces every config file at once.
func (c *Client) ReplaceFiles(ctx context.Context, id string, files []File) (Integration, error) {

	var ret Integration
	r, err := jsonBody(request{route: replaceFiles, path: []string{id}}, map[string][]File{"files": files})
	if err != nil {
		return ret, err
	}
	err = c.call(ctx, r, &ret)
	return ret, err
}

// GetFile reads one config file.
func (c *Client) GetFile(ctx context.Context, id, path string) (File, error) {

	var ret File
	err := c.call(ctx, request{route: getFile, path: []string{id}, query: url.Values{"path": {path}}}, &ret)
	return ret, err
}

// PutFile writes one config file.
func (c *Client) PutFile(ctx context.Context, id, path, content string) (File, error) {

	var ret File
	r, err := jsonBody(request{route: putFile, path: []string{id}, query: url.Values{"path": {path}}}, map[string]string{"content": content})
	if err != nil {
		return ret, err
	}
	err = c.call(ctx, r, &ret)
	return ret, err
}

// DeleteFile deletes one config file.
func (c *Client) DeleteFile(ctx context.Context, id, path string) error {
	return c.call(ctx, request{route: deleteFile, path: []string{id}, query: url.Values{"path": {path}}}, nil)
}

// ListResources lists an integration's resources, contents included.
func (c *Client) ListResources(ctx context.Context, id string) ([]Resource, error) {

	var ret []Resource
	err := c.call(ctx, request{route: listResources, path: []string{id}}, &ret)
	return ret, err
}

// CreateResource adds a resource.
func (c *Client) CreateResource(ctx context.Context, id string, in ResourceInput) (Resource, error) {

	var ret Resource
	r, err := jsonBody(request{route: createResource, path: []string{id}}, in)
	if err != nil {
		return ret, err
	}
	err = c.call(ctx, r, &ret)
	return ret, err
}

// GetResource reads one resource.
func (c *Client) GetResource(ctx context.Context, id, resourceID string) (Resource, error) {

	var ret Resource
	err := c.call(ctx, request{route: getResource, path: []string{id, resourceID}}, &ret)
	return ret, err
}

// UpdateResource replaces a resource.
func (c *Client) UpdateResource(ctx context.Context, id, resourceID string, in ResourceInput) (Resource, error) {

	var ret Resource
	r, err := jsonBody(request{route: updateResource, path: []string{id, resourceID}}, in)
	if err != nil {
		return ret, err
	}
	err = c.call(ctx, r, &ret)
	return ret, err
}

// DeleteResource deletes a resource.
func (c *Client) DeleteResource(ctx context.Context, id, resourceID string) error {
	return c.call(ctx, request{route: deleteResource, path: []string{id, resourceID}}, nil)
}

// ListVersions lists an integration's versions, newest first.
func (c *Client) ListVersions(ctx context.Context, id string) ([]Version, error) {

	var ret []Version
	err := c.call(ctx, request{route: listVersions, path: []string{id}}, &ret)
	return ret, err
}

// CreateVersion freezes the integration as it stands under tag.
func (c *Client) CreateVersion(ctx context.Context, id, tag string) (Version, error) {

	var ret Version
	r, err := jsonBody(request{route: createVersion, path: []string{id}}, map[string]string{"tag": tag})
	if err != nil {
		return ret, err
	}
	err = c.call(ctx, r, &ret)
	return ret, err
}

// DeleteVersion deletes a version nothing is running.
func (c *Client) DeleteVersion(ctx context.Context, id string) error {
	return c.call(ctx, request{route: deleteVersion, path: []string{id}}, nil)
}

// DownloadVersionBundle writes one version's zip into w.
func (c *Client) DownloadVersionBundle(ctx context.Context, id string, w io.Writer, progress Progress) (int64, error) {
	return c.download(ctx, request{route: downloadVersionBundle, path: []string{id}}, w, progress)
}

// ListDeployments lists every deployment, newest first.
func (c *Client) ListDeployments(ctx context.Context) ([]Deployment, error) {

	var ret []Deployment
	err := c.call(ctx, request{route: listDeployments}, &ret)
	return ret, err
}

// ListIntegrationDeployments lists one integration's deployments.
func (c *Client) ListIntegrationDeployments(ctx context.Context, id string) ([]Deployment, error) {

	var ret []Deployment
	err := c.call(ctx, request{route: listIntegrationDeployments, path: []string{id}}, &ret)
	return ret, err
}

// CreateDeployment deploys a version of an integration.
func (c *Client) CreateDeployment(ctx context.Context, id string, settings DeploymentSettings) (Deployment, error) {

	var ret Deployment
	r, err := jsonBody(request{route: createDeployment, path: []string{id}}, settings)
	if err != nil {
		return ret, err
	}
	err = c.call(ctx, r, &ret)
	return ret, err
}

// DeploymentOptions says what a deploy of the integration needs. Empty
// arguments are left out.
func (c *Client) DeploymentOptions(ctx context.Context, id, snapshotID, slug, expose string) (map[string]any, error) {

	q := url.Values{}
	for k, v := range map[string]string{"snapshotId": snapshotID, "slug": slug, "expose": expose} {
		if v != "" {
			q.Set(k, v)
		}
	}
	var ret map[string]any
	err := c.call(ctx, request{route: deploymentOptions, path: []string{id}, query: q}, &ret)
	return ret, err
}

// GetDeployment reads a deployment with its live status.
func (c *Client) GetDeployment(ctx context.Context, id string) (Deployment, error) {

	var ret Deployment
	err := c.call(ctx, request{route: getDeployment, path: []string{id}}, &ret)
	return ret, err
}

// ScaleDeployment changes how many replicas a deployment runs.
func (c *Client) ScaleDeployment(ctx context.Context, id string, replicas int) (Deployment, error) {

	var ret Deployment
	r, err := jsonBody(request{route: scaleDeployment, path: []string{id}}, map[string]int{"replicas": replicas})
	if err != nil {
		return ret, err
	}
	err = c.call(ctx, r, &ret)
	return ret, err
}

// DeleteDeployment undeploys.
func (c *Client) DeleteDeployment(ctx context.Context, id string) error {
	return c.call(ctx, request{route: deleteDeployment, path: []string{id}}, nil)
}

// RolloutDeployment rolls a deployment onto another version, env or tracing
// setting.
func (c *Client) RolloutDeployment(ctx context.Context, id string, rollout Rollout) (Deployment, error) {

	var ret Deployment
	r, err := jsonBody(request{route: rolloutDeployment, path: []string{id}}, rollout)
	if err != nil {
		return ret, err
	}
	err = c.call(ctx, r, &ret)
	return ret, err
}

// DeploymentLogs reads one page of a deployment's stored logs.
func (c *Client) DeploymentLogs(ctx context.Context, id string, q LogQuery) (LogPage, error) {

	values := url.Values{}
	set := func(k, v string) {
		if v != "" {
			values.Set(k, v)
		}
	}
	set("level", q.Level)
	set("q", q.Text)
	set("before", q.Before)
	if !q.From.IsZero() {
		set("from", q.From.UTC().Format(time.RFC3339))
	}
	if !q.To.IsZero() {
		set("to", q.To.UTC().Format(time.RFC3339))
	}
	if q.Limit > 0 {
		set("limit", strconv.Itoa(q.Limit))
	}

	var ret LogPage
	err := c.call(ctx, request{route: deploymentLogs, path: []string{id}, query: values}, &ret)
	return ret, err
}

// PodLogs reads the last lines one pod wrote, as plain text. tail <= 0 lets the
// platform choose.
func (c *Client) PodLogs(ctx context.Context, id, pod string, tail int, w io.Writer) error {

	r := request{route: podLogs, path: []string{id, pod}}
	if tail > 0 {
		r.query = url.Values{"tail": {strconv.Itoa(tail)}}
	}
	_, err := c.download(ctx, r, w, nil)
	return err
}

// StreamPodLogs follows one pod's log, handing every event to fn. It returns
// when the API ends the stream, which it does after thirty minutes; the caller
// reconnects if it wants more.
func (c *Client) StreamPodLogs(ctx context.Context, id, pod string, tail int, fn func(Event) error) error {

	r := request{route: streamPodLogs, path: []string{id, pod}}
	if tail > 0 {
		r.query = url.Values{"tail": {strconv.Itoa(tail)}}
	}
	return c.stream(ctx, r, fn)
}

// OpenAPI is the API's own description of itself.
func (c *Client) OpenAPI(ctx context.Context, w io.Writer) error {

	_, err := c.download(ctx, request{route: openAPI, anonymous: true}, w, nil)
	return err
}

// Healthz says whether the API is up.
func (c *Client) Healthz(ctx context.Context) error {
	return c.call(ctx, request{route: healthz, anonymous: true}, nil)
}

// ProtectedResource is the API's RFC 9728 metadata.
func (c *Client) ProtectedResource(ctx context.Context) (map[string]any, error) {

	var ret map[string]any
	err := c.call(ctx, request{route: protectedResourceMetadata, anonymous: true}, &ret)
	return ret, err
}

// OpenIDConfiguration is the identity provider's discovery document, as the API
// relays it.
func (c *Client) OpenIDConfiguration(ctx context.Context) (map[string]any, error) {

	var ret map[string]any
	err := c.call(ctx, request{route: openidConfiguration, anonymous: true}, &ret)
	return ret, err
}

// AuthorizationServerMetadata is the identity provider's RFC 8414 document, as
// the API relays it.
func (c *Client) AuthorizationServerMetadata(ctx context.Context) (map[string]any, error) {

	var ret map[string]any
	err := c.call(ctx, request{route: authorizationServerMetadata, anonymous: true}, &ret)
	return ret, err
}

// Raw sends any request to the API with your credential and hands back the
// response, error statuses included, for `octopaas api`. path is relative to
// the API's root and may carry a query.
func (c *Client) Raw(ctx context.Context, method, path string, body []byte, contentType string) (*http.Response, error) {

	r := request{route: route{op: "raw", method: method, path: path}, body: body, contentType: contentType}
	resp, err := c.send(ctx, r, false)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized && c.bearer != nil {
		_ = resp.Body.Close()
		return c.send(ctx, r, true)
	}
	return resp, nil
}
