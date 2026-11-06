package environment

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/juancavallotti/octo/orchestrator/internal/kube"
)

// fakeRepo is an in-memory environments table holding the same two uniqueness
// rules as the real one: name and namespace.
type fakeRepo struct {
	rows         map[string]Environment
	integrations map[string]bool
}

func (f *fakeRepo) HasIntegrations(_ context.Context, name string) (bool, error) {
	return f.integrations[name], nil
}

func newFakeRepo() *fakeRepo { return &fakeRepo{rows: map[string]Environment{}} }

func (f *fakeRepo) Create(_ context.Context, e Environment) (Environment, error) {
	if _, ok := f.rows[e.Name]; ok {
		return Environment{}, ErrExists
	}
	for _, row := range f.rows {
		if row.Namespace == e.Namespace {
			return Environment{}, ErrNamespaceTaken
		}
	}
	e.CreatedAt = time.Unix(1, 0)
	f.rows[e.Name] = e
	return e, nil
}

func (f *fakeRepo) Get(_ context.Context, name string) (Environment, error) {
	e, ok := f.rows[name]
	if !ok {
		return Environment{}, ErrNotFound
	}
	return e, nil
}

func (f *fakeRepo) List(_ context.Context) ([]Environment, error) {
	out := make([]Environment, 0, len(f.rows))
	for _, e := range f.rows {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *fakeRepo) Update(_ context.Context, name string, p Patch) (Environment, error) {
	e, ok := f.rows[name]
	if !ok {
		return Environment{}, ErrNotFound
	}
	if p.DisplayName != nil {
		e.DisplayName = *p.DisplayName
	}
	if p.Description != nil {
		e.Description = *p.Description
	}
	f.rows[name] = e
	return e, nil
}

func (f *fakeRepo) Delete(_ context.Context, name string) error {
	if _, ok := f.rows[name]; !ok {
		return ErrNotFound
	}
	delete(f.rows, name)
	return nil
}

// fakeKube records provisioned namespaces by environment.
type fakeKube struct {
	provisioned map[string]string
	inUse       map[string]bool
	ensureErr   error
	deleteErr   error
}

func newFakeKube() *fakeKube {
	return &fakeKube{provisioned: map[string]string{}, inUse: map[string]bool{}}
}

func (f *fakeKube) EnsureNamespace(_ context.Context, environment, namespace string) error {
	if f.ensureErr != nil {
		return f.ensureErr
	}
	f.provisioned[namespace] = environment
	return nil
}

func (f *fakeKube) NamespaceInUse(_ context.Context, namespace string) (bool, error) {
	return f.inUse[namespace], nil
}

func (f *fakeKube) DeleteNamespace(_ context.Context, _, namespace string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.provisioned, namespace)
	return nil
}

const releaseNamespace = "octo-system"

func newTestService() (*Service, *fakeRepo, *fakeKube) {
	repo, k := newFakeRepo(), newFakeKube()
	return NewService(repo, k, releaseNamespace), repo, k
}

func TestCreateProvisionsTheDefaultNamespace(t *testing.T) {
	svc, _, k := newTestService()
	e, err := svc.Create(context.Background(), Input{Name: "staging", DisplayName: "Staging"}, "user-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if e.Namespace != "octo-staging" {
		t.Errorf("namespace = %q, want octo-staging", e.Namespace)
	}
	if e.CreatedBy == nil || *e.CreatedBy != "user-1" {
		t.Errorf("createdBy = %v, want user-1", e.CreatedBy)
	}
	if k.provisioned["octo-staging"] != "staging" {
		t.Errorf("provisioned = %v", k.provisioned)
	}
}

func TestCreateRejects(t *testing.T) {
	cases := []struct {
		name string
		in   Input
		want error
	}{
		{"the default name", Input{Name: Default}, ErrInvalidName},
		{"uppercase", Input{Name: "Staging"}, ErrInvalidName},
		{"a leading hyphen", Input{Name: "-qa"}, ErrInvalidName},
		{"a name over 30", Input{Name: "a234567890123456789012345678901"}, ErrInvalidName},
		{"the empty name", Input{Name: ""}, ErrInvalidName},
		{"a kube- namespace", Input{Name: "qa", Namespace: "kube-system"}, ErrInvalidNamespace},
		{"the default namespace", Input{Name: "qa", Namespace: "default"}, ErrInvalidNamespace},
		{"an invalid namespace", Input{Name: "qa", Namespace: "Not_A_Label"}, ErrInvalidNamespace},
		{"the release namespace", Input{Name: "qa", Namespace: releaseNamespace}, ErrNamespaceTaken},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, k := newTestService()
			if _, err := svc.Create(context.Background(), tc.in, ""); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if len(k.provisioned) != 0 {
				t.Errorf("a refused create provisioned %v", k.provisioned)
			}
		})
	}
}

func TestCreateCollisions(t *testing.T) {
	svc, _, _ := newTestService()
	ctx := context.Background()
	if _, err := svc.Create(ctx, Input{Name: "staging"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, Input{Name: "staging"}, ""); !errors.Is(err, ErrExists) {
		t.Errorf("same name: err = %v, want ErrExists", err)
	}
	if _, err := svc.Create(ctx, Input{Name: "qa", Namespace: "octo-staging"}, ""); !errors.Is(err, ErrNamespaceTaken) {
		t.Errorf("same namespace: err = %v, want ErrNamespaceTaken", err)
	}
}

func TestCreateGivesTheNameBackWhenProvisioningFails(t *testing.T) {
	svc, repo, k := newTestService()
	ctx := context.Background()

	k.ensureErr = kube.ErrNamespaceTaken
	if _, err := svc.Create(ctx, Input{Name: "payments", Namespace: "payments"}, ""); !errors.Is(err, ErrNamespaceTaken) {
		t.Fatalf("err = %v, want ErrNamespaceTaken", err)
	}
	if _, ok := repo.rows["payments"]; ok {
		t.Error("the row survived a failed provision")
	}

	k.ensureErr = nil
	if _, err := svc.Create(ctx, Input{Name: "payments", Namespace: "octo-payments"}, ""); err != nil {
		t.Errorf("retry after a failed provision: %v", err)
	}
}

func TestCreateWithoutACluster(t *testing.T) {
	svc := NewService(newFakeRepo(), nil, releaseNamespace)
	if _, err := svc.Create(context.Background(), Input{Name: "staging"}, ""); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
	if svc.Enabled() {
		t.Error("Enabled() = true without a cluster")
	}
}

func TestListPutsTheDefaultFirst(t *testing.T) {
	svc, _, _ := newTestService()
	ctx := context.Background()
	for _, name := range []string{"staging", "qa"} {
		if _, err := svc.Create(ctx, Input{Name: name}, ""); err != nil {
			t.Fatal(err)
		}
	}
	got, err := svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range got {
		names = append(names, e.Name)
	}
	if len(names) != 3 || names[0] != Default || names[1] != "qa" || names[2] != "staging" {
		t.Fatalf("names = %v, want [octo qa staging]", names)
	}
	if !got[0].IsDefault || got[0].Namespace != releaseNamespace {
		t.Errorf("default = %+v", got[0])
	}
}

func TestTheDefaultCannotBeChanged(t *testing.T) {
	svc, _, _ := newTestService()
	ctx := context.Background()
	name := "Production"
	if _, err := svc.Update(ctx, Default, Patch{DisplayName: &name}); !errors.Is(err, ErrDefault) {
		t.Errorf("update: err = %v, want ErrDefault", err)
	}
	if err := svc.Delete(ctx, Default); !errors.Is(err, ErrDefault) {
		t.Errorf("delete: err = %v, want ErrDefault", err)
	}
	e, err := svc.Get(ctx, Default)
	if err != nil || !e.IsDefault {
		t.Errorf("get default: %+v, %v", e, err)
	}
}

func TestDeleteRefusesWhileWorkloadsRun(t *testing.T) {
	svc, repo, k := newTestService()
	ctx := context.Background()
	if _, err := svc.Create(ctx, Input{Name: "staging"}, ""); err != nil {
		t.Fatal(err)
	}

	k.inUse["octo-staging"] = true
	if err := svc.Delete(ctx, "staging"); !errors.Is(err, ErrInUse) {
		t.Fatalf("err = %v, want ErrInUse", err)
	}
	if _, ok := repo.rows["staging"]; !ok {
		t.Fatal("a refused delete removed the row")
	}

	k.inUse["octo-staging"] = false
	if err := svc.Delete(ctx, "staging"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := k.provisioned["octo-staging"]; ok {
		t.Error("the namespace survived the delete")
	}
	if _, ok := repo.rows["staging"]; ok {
		t.Error("the row survived the delete")
	}
}

func TestDeleteRefusesWhileIntegrationsAreDefined(t *testing.T) {
	svc, repo, k := newTestService()
	ctx := context.Background()
	if _, err := svc.Create(ctx, Input{Name: "staging"}, ""); err != nil {
		t.Fatal(err)
	}
	repo.integrations = map[string]bool{"staging": true}
	if err := svc.Delete(ctx, "staging"); !errors.Is(err, ErrInUse) {
		t.Fatalf("err = %v, want ErrInUse", err)
	}
	if _, ok := k.provisioned["octo-staging"]; !ok {
		t.Error("a refused delete removed the namespace")
	}
}

func TestDeleteLeavesAForeignNamespaceButRemovesTheRow(t *testing.T) {
	svc, repo, k := newTestService()
	ctx := context.Background()
	if _, err := svc.Create(ctx, Input{Name: "staging"}, ""); err != nil {
		t.Fatal(err)
	}
	k.deleteErr = kube.ErrNamespaceTaken
	if err := svc.Delete(ctx, "staging"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := repo.rows["staging"]; ok {
		t.Error("the row survived")
	}
}

func TestUpdateChangesOnlyWhatIsGiven(t *testing.T) {
	svc, _, _ := newTestService()
	ctx := context.Background()
	if _, err := svc.Create(ctx, Input{Name: "staging", DisplayName: "Staging", Description: "pre-prod"}, ""); err != nil {
		t.Fatal(err)
	}
	name := "Stage"
	e, err := svc.Update(ctx, "staging", Patch{DisplayName: &name})
	if err != nil {
		t.Fatal(err)
	}
	if e.DisplayName != "Stage" || e.Description != "pre-prod" || e.Namespace != "octo-staging" {
		t.Errorf("after update: %+v", e)
	}
}

func TestExists(t *testing.T) {
	svc, _, _ := newTestService()
	ctx := context.Background()
	if _, err := svc.Create(ctx, Input{Name: "staging"}, ""); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{"": true, Default: true, "staging": true, "qa": false} {
		if got, err := svc.Exists(ctx, name); err != nil || got != want {
			t.Errorf("Exists(%q) = %v, %v; want %v", name, got, err, want)
		}
	}
}

type recordingRegistry map[string]string

func (r recordingRegistry) RegisterEnvironment(environment, namespace string) {
	r[environment] = namespace
}
func (r recordingRegistry) ForgetEnvironment(environment string) { delete(r, environment) }

func TestTheRegistryFollowsEnvironments(t *testing.T) {
	svc, repo, _ := newTestService()
	ctx := context.Background()
	repo.rows["existing"] = Environment{Name: "existing", Namespace: "octo-existing"}
	reg := recordingRegistry{}
	svc.WithRegistry(reg)

	if err := svc.Register(ctx); err != nil {
		t.Fatal(err)
	}
	if reg["existing"] != "octo-existing" {
		t.Errorf("a stored environment was not registered: %v", reg)
	}
	if _, err := svc.Create(ctx, Input{Name: "staging"}, ""); err != nil {
		t.Fatal(err)
	}
	if reg["staging"] != "octo-staging" {
		t.Errorf("a created environment was not registered: %v", reg)
	}
	if err := svc.Delete(ctx, "staging"); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg["staging"]; ok {
		t.Errorf("a deleted environment is still registered: %v", reg)
	}

	ns, ok, err := svc.Namespace(ctx, "existing")
	if err != nil || !ok || ns != "octo-existing" {
		t.Errorf("Namespace(existing) = %q, %v, %v", ns, ok, err)
	}
	if _, ok, _ := svc.Namespace(ctx, "missing"); ok {
		t.Error("Namespace found a missing environment")
	}
}
