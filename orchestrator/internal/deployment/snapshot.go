package deployment

import "encoding/json"

// DeploymentsSubject is the NATS subject an integration's deployment snapshots are
// published on. Internal infrastructure subjects are namespaced under "internal."
func DeploymentsSubject(integrationID string) string {
	return "internal.deployments." + integrationID
}

// MarshalSnapshot encodes deployments as the JSON array the deployments stream
// carries — the same wire shape the REST list returns, so the browser parses a
// streamed frame and a polled response identically. current is the runtime
// release this install deploys.
func MarshalSnapshot(items []Deployment, current string) ([]byte, error) {
	out := make([]deploymentResponse, 0, len(items))
	for _, d := range items {
		out = append(out, toResponse(d, current))
	}
	return json.Marshal(out)
}
