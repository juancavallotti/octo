package client

import (
	"net/http"
	"net/url"
	"strings"
)

// route is one operation of the public API, as api/static/openapi.yaml names it.
type route struct {
	op     string
	method string
	path   string
}

// expand fills the route's {placeholders}, in order, with values escaped for a
// path segment.
func (r route) expand(values ...string) string {

	ret := r.path
	for _, v := range values {
		open := strings.IndexByte(ret, '{')
		end := strings.IndexByte(ret, '}')
		if open < 0 || end < open {
			break
		}
		ret = ret[:open] + url.PathEscape(v) + ret[end+1:]
	}
	return ret
}

// Every operation the API serves. This table is the whole of what the client
// can call, and specdrift_test.go holds it against the spec in both directions.
var (
	getMe = route{"getMe", http.MethodGet, "/v1/me"}

	listIntegrations  = route{"listIntegrations", http.MethodGet, "/v1/integrations"}
	createIntegration = route{"createIntegration", http.MethodPost, "/v1/integrations"}
	getIntegration    = route{"getIntegration", http.MethodGet, "/v1/integrations/{id}"}
	renameIntegration = route{"renameIntegration", http.MethodPatch, "/v1/integrations/{id}"}
	deleteIntegration = route{"deleteIntegration", http.MethodDelete, "/v1/integrations/{id}"}
	downloadBundle    = route{"downloadBundle", http.MethodGet, "/v1/integrations/{id}/bundle"}
	replaceBundle     = route{"replaceBundle", http.MethodPut, "/v1/integrations/{id}/bundle"}
	importBundle      = route{"importBundle", http.MethodPost, "/v1/integrations/bundle"}

	listFiles    = route{"listFiles", http.MethodGet, "/v1/integrations/{id}/files"}
	replaceFiles = route{"replaceFiles", http.MethodPut, "/v1/integrations/{id}/files"}
	getFile      = route{"getFile", http.MethodGet, "/v1/integrations/{id}/files/content"}
	putFile      = route{"putFile", http.MethodPut, "/v1/integrations/{id}/files/content"}
	deleteFile   = route{"deleteFile", http.MethodDelete, "/v1/integrations/{id}/files/content"}

	listResources  = route{"listResources", http.MethodGet, "/v1/integrations/{id}/resources"}
	createResource = route{"createResource", http.MethodPost, "/v1/integrations/{id}/resources"}
	getResource    = route{"getResource", http.MethodGet, "/v1/integrations/{id}/resources/{resourceId}"}
	updateResource = route{"updateResource", http.MethodPut, "/v1/integrations/{id}/resources/{resourceId}"}
	deleteResource = route{"deleteResource", http.MethodDelete, "/v1/integrations/{id}/resources/{resourceId}"}

	listVersions          = route{"listVersions", http.MethodGet, "/v1/integrations/{id}/versions"}
	createVersion         = route{"createVersion", http.MethodPost, "/v1/integrations/{id}/versions"}
	deleteVersion         = route{"deleteVersion", http.MethodDelete, "/v1/versions/{id}"}
	downloadVersionBundle = route{"downloadVersionBundle", http.MethodGet, "/v1/versions/{id}/bundle"}

	listDeployments            = route{"listDeployments", http.MethodGet, "/v1/deployments"}
	listIntegrationDeployments = route{"listIntegrationDeployments", http.MethodGet, "/v1/integrations/{id}/deployments"}
	createDeployment           = route{"createDeployment", http.MethodPost, "/v1/integrations/{id}/deployments"}
	deploymentOptions          = route{"deploymentOptions", http.MethodGet, "/v1/integrations/{id}/deployments/options"}
	getDeployment              = route{"getDeployment", http.MethodGet, "/v1/deployments/{id}"}
	scaleDeployment            = route{"scaleDeployment", http.MethodPatch, "/v1/deployments/{id}"}
	deleteDeployment           = route{"deleteDeployment", http.MethodDelete, "/v1/deployments/{id}"}
	rolloutDeployment          = route{"rolloutDeployment", http.MethodPost, "/v1/deployments/{id}/rollout"}

	deploymentLogs = route{"deploymentLogs", http.MethodGet, "/v1/deployments/{id}/logs"}
	podLogs        = route{"podLogs", http.MethodGet, "/v1/deployments/{id}/pods/{pod}/logs"}
	streamPodLogs  = route{"streamPodLogs", http.MethodGet, "/v1/deployments/{id}/pods/{pod}/logs/stream"}

	openAPI                     = route{"openapi", http.MethodGet, "/openapi.yaml"}
	healthz                     = route{"healthz", http.MethodGet, "/healthz"}
	protectedResourceMetadata   = route{"protectedResourceMetadata", http.MethodGet, "/.well-known/oauth-protected-resource"}
	openidConfiguration         = route{"openidConfiguration", http.MethodGet, "/.well-known/openid-configuration"}
	authorizationServerMetadata = route{"authorizationServerMetadata", http.MethodGet, "/.well-known/oauth-authorization-server"}
)

var routes = []route{
	getMe,
	listIntegrations, createIntegration, getIntegration, renameIntegration, deleteIntegration,
	downloadBundle, replaceBundle, importBundle,
	listFiles, replaceFiles, getFile, putFile, deleteFile,
	listResources, createResource, getResource, updateResource, deleteResource,
	listVersions, createVersion, deleteVersion, downloadVersionBundle,
	listDeployments, listIntegrationDeployments, createDeployment, deploymentOptions,
	getDeployment, scaleDeployment, deleteDeployment, rolloutDeployment,
	deploymentLogs, podLogs, streamPodLogs,
	openAPI, healthz, protectedResourceMetadata, openidConfiguration, authorizationServerMetadata,
}
