package kube

import (
	"context"
	"errors"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func testRunSpec() TestRunSpec {
	return TestRunSpec{
		ID:            "22222222-2222-4222-8222-222222222222",
		DeploymentID:  "dep-1",
		IntegrationID: "int-1",
		Definition:    "flows: []",
		Files: map[string]string{
			".octo/tests/orders_test.yaml": "flow: orders",
			"templates/welcome.tmpl":       "hi",
		},
		Suites:    []string{".octo/tests/orders_test.yaml"},
		Env:       map[string]string{"REGION": "eu"},
		SecretEnv: map[string]string{"API_KEY": "API_KEY"},
	}
}

func TestApplyTestRun(t *testing.T) {
	c := devRunClient()
	spec := testRunSpec()
	if err := c.ApplyTestRun(context.Background(), spec); err != nil {
		t.Fatalf("ApplyTestRun: %v", err)
	}
	job, err := c.clientset.BatchV1().Jobs(testNamespace).Get(context.Background(), testRunName(spec.ID), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	// A test pod must never be mistaken for one of the deployment's replicas.
	if _, ok := job.Spec.Template.Labels[labelDeploymentID]; ok {
		t.Errorf("test pod carries %s", labelDeploymentID)
	}
	ctr := job.Spec.Template.Spec.Containers[0]
	if ctr.Image != "octo-devruntime:dev" {
		t.Errorf("image = %q", ctr.Image)
	}
	args := strings.Join(ctr.Args, " ")
	if args != "test /work/config/.octo/tests/orders_test.yaml --config /work/config --report-json /dev/stdout" {
		t.Errorf("args = %q", args)
	}
	if !hasSecretEnv(ctr.Env, "API_KEY") {
		t.Errorf("env = %v, want API_KEY from a secret", ctr.Env)
	}

	cm, err := c.clientset.CoreV1().ConfigMaps(testNamespace).Get(context.Background(), testRunName(spec.ID), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get configmap: %v", err)
	}
	if len(cm.OwnerReferences) != 1 || cm.OwnerReferences[0].Kind != "Job" {
		t.Errorf("owner = %v, want the job", cm.OwnerReferences)
	}
	paths := map[string]string{}
	for _, it := range job.Spec.Template.Spec.Volumes[0].ConfigMap.Items {
		paths[it.Path] = cm.Data[it.Key]
	}
	if paths[configFileName] != "flows: []" || paths["templates/welcome.tmpl"] != "hi" {
		t.Errorf("projected files = %v", paths)
	}
}

func hasSecretEnv(env []corev1.EnvVar, name string) bool {
	for _, e := range env {
		if e.Name == name && e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
			return true
		}
	}
	return false
}

func TestTestBundleRefusals(t *testing.T) {
	if _, _, err := testBundle("x", map[string]string{configFileName: "y"}); err == nil {
		t.Error("a resource named like the definition was accepted")
	}
	big := strings.Repeat("a", maxTestBundleBytes)
	if _, _, err := testBundle(big, nil); !errors.Is(err, ErrTestBundleTooLarge) {
		t.Errorf("err = %v, want ErrTestBundleTooLarge", err)
	}
}

func TestTestRunState(t *testing.T) {
	failed := func(reason string) *batchv1.Job {
		return &batchv1.Job{Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{
			Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: reason,
		}}}}
	}
	pod := func(state corev1.ContainerState) corev1.Pod {
		return corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "p"},
			Status:     corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: TestContainer, State: state}}},
		}
	}
	exited := func(code int32) corev1.Pod {
		return pod(corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: code}})
	}
	cases := []struct {
		name string
		job  *batchv1.Job
		pods []corev1.Pod
		want TestRunState
	}{
		{"running", &batchv1.Job{}, nil, TestRunState{Phase: TestRunActive}},
		{"exited 1", failed("BackoffLimitExceeded"), []corev1.Pod{exited(1)}, TestRunState{Phase: TestRunExited, ExitCode: 1, Pod: "p"}},
		{"exited 0", &batchv1.Job{}, []corev1.Pod{exited(0)}, TestRunState{Phase: TestRunExited, Pod: "p"}},
		{"deadline wins", failed("DeadlineExceeded"), []corev1.Pod{exited(137)}, TestRunState{Phase: TestRunDeadlineExceeded, Pod: "p"}},
		{"image will not pull", &batchv1.Job{}, []corev1.Pod{pod(corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff", Message: "nope"},
		})}, TestRunState{Phase: TestRunFailedToStart, Pod: "p", Reason: "ImagePullBackOff: nope"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := testRunState(tc.job, tc.pods); got != tc.want {
				t.Errorf("state = %+v, want %+v", got, tc.want)
			}
		})
	}
}
