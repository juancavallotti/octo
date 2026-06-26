package services_test

import (
	"context"
	"testing"

	"github.com/juancavallotti/octo/services"
	_ "github.com/juancavallotti/octo/services/standalone" // self-registers as "standalone"
)

// With RUNTIME_SERVICES_MODULE unset, the default module is selected and the
// blank-imported standalone provider registers itself as active.
func TestNewSelectsDefaultModule(t *testing.T) {
	if services.Module() != services.DefaultModule {
		t.Fatalf("Module() = %q, want default %q", services.Module(), services.DefaultModule)
	}

	svc, err := services.New(context.Background())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := svc.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()

	lease, err := svc.LeaderElection().Acquire(context.Background(), "k")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if !lease.IsLeader() {
		t.Fatal("standalone provider should always be the leader")
	}
}
