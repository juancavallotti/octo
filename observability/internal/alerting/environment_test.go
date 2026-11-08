package alerting

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/juancavallotti/octo/observability/internal/environment/current"
)

type deploymentsIn map[string]string

func (d deploymentsIn) DeploymentEnvironment(_ context.Context, id string) (string, bool, error) {
	env, ok := d[id]
	return env, ok, nil
}

func topicTo(deploymentID string) ActionSpec {
	params, _ := json.Marshal(map[string]string{"deploymentId": deploymentID, "subject": "alerts"})
	return ActionSpec{ID: "a1", Type: ActionTypeTopic, Params: params}
}

// The app an alert is handed to acts with an identity bound to its own
// environment, so a watch may only hand alerts to an app in the watch's own.
func TestATopicActionStaysInTheWatchesEnvironment(t *testing.T) {
	s := (&Service{}).WithDeployments(deploymentsIn{"agent-default": "", "agent-staging": "staging"})
	staging := current.With(context.Background(), "staging", true)

	if err := s.actionsStayInEnvironment(staging, Watch{Actions: []ActionSpec{topicTo("agent-staging")}}); err != nil {
		t.Errorf("an app in the same environment: %v", err)
	}
	err := s.actionsStayInEnvironment(staging, Watch{Actions: []ActionSpec{topicTo("agent-default")}})
	if !errors.Is(err, ErrInvalidParams) {
		t.Errorf("an app in another environment: err = %v, want ErrInvalidParams", err)
	}
	if err := s.actionsStayInEnvironment(context.Background(), Watch{Actions: []ActionSpec{topicTo("agent-default")}}); err != nil {
		t.Errorf("the default to the default: %v", err)
	}
}
