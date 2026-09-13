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
// container names which container to read; empty means the pod's only one. A pod with
// more than one makes an unnamed request ambiguous, which Kubernetes rejects, so such
// a caller names the container it wants.
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
