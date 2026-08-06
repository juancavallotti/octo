package kube

import (
	"context"
	"io"

	corev1 "k8s.io/api/core/v1"
)

// PodLogs opens a stream of a pod's container logs. With follow set, the stream
// stays open and delivers new lines as they are written (tailing) until the
// caller closes it or the context is cancelled; otherwise it returns the current
// buffer and ends. tail, when > 0, limits the initial replay to the last N lines
// so a long-running pod doesn't dump its whole history on connect.
//
// container names which container to read; empty means the pod's only one, which is
// the case for a deployment. A dev-run pod has two — the runtime and the sidecar
// that owns its workspace — and Kubernetes rejects an ambiguous request, so that
// caller names the runtime.
//
// The returned ReadCloser is the raw log byte stream (plain text, newline
// separated); the caller owns closing it. Pod names come from Status().Pods.
func (c *Client) PodLogs(
	ctx context.Context, podName, container string, follow bool, tail int64,
) (io.ReadCloser, error) {
	opts := &corev1.PodLogOptions{Follow: follow, Container: container}
	if tail > 0 {
		opts.TailLines = &tail
	}
	return c.clientset.CoreV1().Pods(c.namespace).GetLogs(podName, opts).Stream(ctx)
}

// RuntimeContainer is the container in a workload pod running octo. Exported
// because a dev-run pod has two containers and callers streaming its logs have to
// say which — and this is the one anyone asking for "the logs" means.
const RuntimeContainer = "runtime"
