package integration

import (
	"context"
	"testing"
)

// The icon is a choice, so the thing worth proving is that it survives the
// writes that know nothing about it — an editor save, a bundle replace and the
// agent's republish all go through Update with a whole integration.
func TestIconSurvivesAnOrdinaryUpdate(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	created := createIntegration(t, r, "icon-survives-update", "flows:\n  - name: intake\n")
	if created.Icon != "" {
		t.Errorf("a new integration starts with icon %q, want empty (derive it)", created.Icon)
	}

	withIcon, err := r.SetIcon(ctx, created.ID, "Slack", "")
	if err != nil {
		t.Fatalf("set icon: %v", err)
	}
	if withIcon.Icon != "Slack" {
		t.Fatalf("icon = %q, want Slack", withIcon.Icon)
	}

	updated, err := r.Update(ctx, created.ID, "icon-survives-update", "flows:\n  - name: changed\n", "")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Icon != "Slack" {
		t.Errorf("an ordinary update cleared the icon (got %q)", updated.Icon)
	}
}

// Clearing has to mean "derive again", not "freeze whatever is derived today" —
// otherwise the feature quietly turns the derivation off for anything it touches.
func TestClearingTheIconGoesBackToDeriving(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	created := createIntegration(t, r, "icon-clears", "flows:\n  - name: intake\n")
	if _, err := r.SetIcon(ctx, created.ID, "Slack", ""); err != nil {
		t.Fatalf("set icon: %v", err)
	}

	cleared, err := r.SetIcon(ctx, created.ID, "", "")
	if err != nil {
		t.Fatalf("clear icon: %v", err)
	}
	if cleared.Icon != "" {
		t.Errorf("icon = %q, want empty so the UI derives one", cleared.Icon)
	}
}

func TestSetIconOnAnUnknownIntegration(t *testing.T) {
	r := newTestRepo(t)
	if _, err := r.SetIcon(context.Background(), nonexistentID, "Slack", ""); err != ErrNotFound {
		t.Errorf("set icon on a missing integration = %v, want ErrNotFound", err)
	}
}
