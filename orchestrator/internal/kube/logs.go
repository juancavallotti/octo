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
// The returned ReadCloser is the raw log byte stream (plain text, newline
// separated); the caller owns closing it. Pod names come from Status().Pods.
func (c *Client) PodLogs(ctx context.Context, podName string, follow bool, tail int64) (io.ReadCloser, error) {
	opts := &corev1.PodLogOptions{Follow: follow}
	if tail > 0 {
		opts.TailLines = &tail
	}
	return c.clientset.CoreV1().Pods(c.namespace).GetLogs(podName, opts).Stream(ctx)
}
