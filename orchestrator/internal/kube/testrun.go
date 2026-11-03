package kube

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	labelTestRunID = "octo.dev/test-run-id"
	// labelTestOf names the deployment a test run tests. Deliberately not
	// labelDeploymentID: everything carrying that label is counted as the
	// deployment's own workload, and a test pod is not one of its replicas.
	labelTestOf = "octo.dev/test-of"

	// TestContainer is the container in a test-run pod running dolphin.
	TestContainer = "dolphin"

	testConfigVolume = "config"
	testConfigDir    = "/work/config"
	testTmpVolume    = "tmp"
	testTmpDir       = "/tmp"
	testDolphinBin   = "/usr/local/bin/dolphin"
	testOctoBin      = "/usr/local/bin/octo"

	// testRunDeadlineSeconds bounds a whole test run, after which Kubernetes kills
	// it and the run is timed out.
	testRunDeadlineSeconds = 300
	// testRunTTLSeconds is how long a finished Job, its pod and its logs are kept
	// for the result to be read back.
	testRunTTLSeconds = 3600
	// maxTestBundleBytes keeps a test bundle inside what one ConfigMap holds (1MiB),
	// with room for the object's own metadata.
	maxTestBundleBytes = 900 * 1024
)

// ErrTestBundleTooLarge is returned when an integration's definition, resources
// and suites together do not fit the ConfigMap a test run ships them in.
var ErrTestBundleTooLarge = errors.New("kube: the integration is too large to test")

// TestRunDeadline is how long a test run may take before it is killed.
const TestRunDeadline = testRunDeadlineSeconds * time.Second

// TestRunSpec describes one run of an integration's suites.
type TestRunSpec struct {
	ID            string // test run uuid; drives resource names
	DeploymentID  string // the deployment whose snapshot and env are being tested
	IntegrationID string
	// Definition is the runtime-loadable integration YAML under test.
	Definition string
	// Files are its resources and suites, keyed by path relative to the config
	// directory the definition is loaded from.
	Files map[string]string
	// Suites are the paths in Files dolphin runs, relative like Files' keys.
	Suites    []string
	Env       map[string]string // literal env vars, as a deployment's pods get them
	SecretEnv map[string]string // env-var name → cluster-secret key, as a deployment's pods get them
}

// TestRunPhase is where a test run's Job is.
type TestRunPhase string

const (
	// TestRunActive is a run still going, or not started yet.
	TestRunActive TestRunPhase = "active"
	// TestRunExited is a run whose dolphin exited; ExitCode says how.
	TestRunExited TestRunPhase = "exited"
	// TestRunDeadlineExceeded is a run killed for taking too long.
	TestRunDeadlineExceeded TestRunPhase = "deadline"
	// TestRunGone is a run whose Job no longer exists.
	TestRunGone TestRunPhase = "gone"
	// TestRunFailedToStart is a run whose pod never ran dolphin (an image that would
	// not pull, a secret that is missing).
	TestRunFailedToStart TestRunPhase = "failed-to-start"
)

// TestRunState is what the cluster says about a test run.
type TestRunState struct {
	Phase    TestRunPhase
	ExitCode int32
	// Pod is the pod that ran dolphin, "" when there is none to read logs from.
	Pod string
	// Reason is the cluster's explanation when the run did not exit on its own.
	Reason string
}

// TestImage is the image test runs execute in: the standalone runtime, which
// carries dolphin beside the octo it drives.
func (c *Client) TestImage() string { return c.devRuntimeImage }

// TestRunsEnabled reports whether this install can run tests: it needs the
// standalone runtime image.
func (c *Client) TestRunsEnabled() bool { return c.devRuntimeImage != "" }

// ApplyTestRun creates the Job for spec and the ConfigMap holding its files. The
// ConfigMap is owned by the Job, so the Job's TTL collects both.
func (c *Client) ApplyTestRun(ctx context.Context, spec TestRunSpec) error {
	data, items, err := testBundle(spec.Definition, spec.Files)
	if err != nil {
		return err
	}
	name := testRunName(spec.ID)
	lbls := map[string]string{
		labelManagedBy:     managedByValue,
		labelTestRunID:     spec.ID,
		labelTestOf:        spec.DeploymentID,
		labelIntegrationID: spec.IntegrationID,
	}
	job, err := c.clientset.BatchV1().Jobs(c.namespace).Create(ctx, c.testRunJob(name, lbls, spec, items), metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("kube: create test job: %w", err)
	}
	owner := metav1.OwnerReference{
		APIVersion: "batch/v1",
		Kind:       "Job",
		Name:       job.Name,
		UID:        job.UID,
	}
	if _, err := c.clientset.CoreV1().ConfigMaps(c.namespace).Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: lbls, OwnerReferences: []metav1.OwnerReference{owner}},
		Data:       data,
	}, metav1.CreateOptions{}); err != nil {
		_ = c.DeleteTestRun(ctx, spec.ID)
		return fmt.Errorf("kube: create test configmap: %w", err)
	}
	return nil
}

// TestRunState reads where a test run is. A Job that no longer exists is
// TestRunGone rather than an error, since a TTL'd Job is an expected end.
func (c *Client) TestRunState(ctx context.Context, runID string) (TestRunState, error) {
	job, err := c.clientset.BatchV1().Jobs(c.namespace).Get(ctx, testRunName(runID), metav1.GetOptions{})
	if err != nil {
		if ignoreNotFound(err) == nil {
			return TestRunState{Phase: TestRunGone}, nil
		}
		return TestRunState{}, fmt.Errorf("kube: get test job: %w", err)
	}
	pods, err := c.clientset.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelTestRunID + "=" + runID,
	})
	if err != nil {
		return TestRunState{}, fmt.Errorf("kube: list test pods: %w", err)
	}
	return testRunState(job, pods.Items), nil
}

// TestRunLogs reads everything the test pod's dolphin wrote.
func (c *Client) TestRunLogs(ctx context.Context, pod string) ([]byte, error) {
	stream, err := c.PodLogs(ctx, pod, TestContainer, false, 0)
	if err != nil {
		return nil, fmt.Errorf("kube: open test logs: %w", err)
	}
	defer func() { _ = stream.Close() }()
	ret, err := io.ReadAll(stream)
	if err != nil {
		return nil, fmt.Errorf("kube: read test logs: %w", err)
	}
	return ret, nil
}

// DeleteTestRun removes a test run's Job, its pod and, through ownership, its
// ConfigMap. A run already gone is not an error.
func (c *Client) DeleteTestRun(ctx context.Context, runID string) error {
	background := metav1.DeletePropagationBackground
	err := c.clientset.BatchV1().Jobs(c.namespace).Delete(ctx, testRunName(runID), metav1.DeleteOptions{
		PropagationPolicy: &background,
	})
	return ignoreNotFound(err)
}

// testRunState reduces a Job and its pods to a TestRunState.
//
// The Job's conditions come first because they are what says a run is over:
// a DeadlineExceeded failure is a timeout whatever the pod reports. Otherwise the
// dolphin container's own termination is the answer, since dolphin's exit code
// is the verdict (0 passed, 1 failed, 2 could not run).
func testRunState(job *batchv1.Job, pods []corev1.Pod) TestRunState {
	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue && cond.Reason == "DeadlineExceeded" {
			return TestRunState{Phase: TestRunDeadlineExceeded, Pod: firstPod(pods), Reason: cond.Message}
		}
	}
	for _, p := range pods {
		for _, cs := range p.Status.ContainerStatuses {
			if cs.Name != TestContainer {
				continue
			}
			if t := cs.State.Terminated; t != nil {
				return TestRunState{Phase: TestRunExited, ExitCode: t.ExitCode, Pod: p.Name}
			}
			if w := cs.State.Waiting; w != nil && startFailure(w.Reason) {
				return TestRunState{Phase: TestRunFailedToStart, Pod: p.Name, Reason: w.Reason + ": " + w.Message}
			}
		}
	}
	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			return TestRunState{Phase: TestRunFailedToStart, Pod: firstPod(pods), Reason: cond.Reason + ": " + cond.Message}
		}
	}
	return TestRunState{Phase: TestRunActive}
}

// startFailure reports whether a waiting container's reason means it will not
// start without someone changing something. A pull that is backing off is not
// one: the kubelet keeps retrying it, and the run's deadline is what ends it.
func startFailure(reason string) bool {
	switch reason {
	case "InvalidImageName", "CreateContainerConfigError", "CreateContainerError":
		return true
	}
	return false
}

// firstPod is the name of a pod to read logs from, "" when there is none.
func firstPod(pods []corev1.Pod) string {
	if len(pods) == 0 {
		return ""
	}
	return pods[0].Name
}

// testBundle lays the definition and files out as ConfigMap data. ConfigMap keys
// cannot hold '/', so each file is stored under a numbered key and projected back
// to its path.
func testBundle(definition string, extra map[string]string) (map[string]string, []corev1.KeyToPath, error) {
	if _, clash := extra[configFileName]; clash {
		return nil, nil, fmt.Errorf("kube: a resource is named %s, which is where the definition goes", configFileName)
	}
	files := make(map[string]string, len(extra)+1)
	for p, content := range extra {
		files[p] = content
	}
	files[configFileName] = definition
	paths := make([]string, 0, len(files))
	size := 0
	for p, content := range files {
		paths = append(paths, p)
		size += len(p) + len(content)
	}
	if size > maxTestBundleBytes {
		return nil, nil, fmt.Errorf("%w: %d bytes, the limit is %d", ErrTestBundleTooLarge, size, maxTestBundleBytes)
	}
	sort.Strings(paths)
	data := make(map[string]string, len(paths))
	items := make([]corev1.KeyToPath, 0, len(paths))
	for i, p := range paths {
		key := fmt.Sprintf("f%d", i)
		data[key] = files[p]
		items = append(items, corev1.KeyToPath{Key: key, Path: p})
	}
	return data, items, nil
}

// testRunJob is the Job running dolphin over spec's suites, against the
// standalone octo in the same image.
func (c *Client) testRunJob(name string, lbls map[string]string, spec TestRunSpec, items []corev1.KeyToPath) *batchv1.Job {
	backoff := int32(0)
	noToken := false
	deadline := int64(testRunDeadlineSeconds)
	ttl := int32(testRunTTLSeconds)

	args := []string{"test"}
	for _, s := range spec.Suites {
		args = append(args, path.Join(testConfigDir, s))
	}
	args = append(args, "--config", testConfigDir, "--report-json", "/dev/stdout")

	env := append(containerEnv(Spec{Env: spec.Env, SecretEnv: spec.SecretEnv}),
		corev1.EnvVar{Name: "OCTO_PATH", Value: testOctoBin},
		corev1.EnvVar{Name: "TMPDIR", Value: testTmpDir},
	)

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: lbls},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff,
			ActiveDeadlineSeconds:   &deadline,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: lbls},
				Spec: corev1.PodSpec{
					// No ServiceAccount, like a dev run: the standalone build talks to no
					// cluster API, so the pod carries no credential at all.
					RestartPolicy:                corev1.RestartPolicyNever,
					AutomountServiceAccountToken: &noToken,
					ImagePullSecrets:             c.pullSecretRefs(),
					Containers: []corev1.Container{{
						Name:            TestContainer,
						Image:           c.devRuntimeImage,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Command:         []string{testDolphinBin},
						Args:            args,
						Env:             env,
						VolumeMounts: []corev1.VolumeMount{
							{Name: testConfigVolume, MountPath: testConfigDir, ReadOnly: true},
							{Name: testTmpVolume, MountPath: testTmpDir},
						},
						SecurityContext: restricted(),
					}},
					Volumes: []corev1.Volume{
						{Name: testConfigVolume, VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: name},
								Items:                items,
							},
						}},
						{Name: testTmpVolume, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
					},
				},
			},
		},
	}
}

func testRunName(runID string) string { return "octo-test-" + runID }
