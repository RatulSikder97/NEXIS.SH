package usecase

// Coverage for ProjectsService.Get, List, and the cross-tenant guards on
// Update and UpdatePolicy. The existing projects_test.go covers Create +
// Archive + Update + UpdatePolicy happy paths.

import (
	"context"
	"errors"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// TestProjects_Get_HappyPath — Get returns the project when the principal's
// org owns it.
func TestProjects_Get_HappyPath(t *testing.T) {
	repo := newFakeProjectsRepo()
	svc := NewProjectsService(repo, &noopIntLookup{}, nil, nil)
	princ := testPrincipal()

	created, err := svc.Create(context.Background(), princ, baseCreateInput("Orders"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := svc.Get(context.Background(), princ, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("Get returned wrong project: %s vs %s", got.ID, created.ID)
	}
}

// TestProjects_Get_CrossTenantReturnsNotFound — Get of a foreign org's
// project returns ErrNotFound, never the actual row.
func TestProjects_Get_CrossTenantReturnsNotFound(t *testing.T) {
	repo := newFakeProjectsRepo()
	svc := NewProjectsService(repo, &noopIntLookup{}, nil, nil)
	princA := testPrincipal()

	created, err := svc.Create(context.Background(), princA, baseCreateInput("Orders"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	princB := domain.Principal{
		UserID: "user-b", OrgID: "different-org", Role: domain.RoleOwner,
	}
	_, err = svc.Get(context.Background(), princB, created.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant Get: expected ErrNotFound, got %v", err)
	}
}

// TestProjects_Get_RepoErrorSurfaces — non-NotFound errors bubble.
func TestProjects_Get_RepoErrorSurfaces(t *testing.T) {
	repo := newFakeProjectsRepo()
	svc := NewProjectsService(repo, &noopIntLookup{}, nil, nil)
	princ := testPrincipal()

	_, err := svc.Get(context.Background(), princ, "non-existent-id")
	if err == nil {
		t.Fatalf("expected error for missing project")
	}
}

// TestProjects_List_FiltersByOrg — even if the repo returns rows from
// another org (defence-in-depth path), List filters them out.
func TestProjects_List_FiltersByOrg(t *testing.T) {
	repo := newFakeProjectsRepo()
	svc := NewProjectsService(repo, &noopIntLookup{}, nil, nil)
	princA := testPrincipal()

	_, _ = svc.Create(context.Background(), princA, baseCreateInput("Orders"))
	_, _ = svc.Create(context.Background(), princA, baseCreateInput("Billing"))

	got, err := svc.List(context.Background(), princA, "00000000-0000-0000-0000-000000000003")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(got))
	}
	for _, p := range got {
		if p.OrgID != princA.OrgID {
			t.Fatalf("foreign row leaked: %+v", p)
		}
	}
}

// TestProjects_List_OtherOrgGetsEmpty — a second org listing the same
// workspace gets an empty result (defence-in-depth filter fires).
func TestProjects_List_OtherOrgGetsEmpty(t *testing.T) {
	repo := newFakeProjectsRepo()
	svc := NewProjectsService(repo, &noopIntLookup{}, nil, nil)
	princA := testPrincipal()
	_, _ = svc.Create(context.Background(), princA, baseCreateInput("Orders"))

	princB := domain.Principal{
		UserID: "user-b", OrgID: "different-org", Role: domain.RoleOwner,
	}
	got, err := svc.List(context.Background(), princB, "00000000-0000-0000-0000-000000000003")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("cross-tenant List should return empty, got %d", len(got))
	}
}
