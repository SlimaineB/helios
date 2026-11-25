package api

import (
	"context"

	"github.com/slim/helios/server/internal/gitops"
	"github.com/slim/helios/server/internal/model"
)

func managedOwnerKey(kind, name string) string { return kind + "/" + name }

func repoManagedKey(project, url string) string { return project + "::" + url }

func assignmentManagedKey(instanceID, appName string) string {
	return instanceID + "::" + appName
}

func (s *Server) managedOwners(ctx context.Context) (map[string]model.ManagedOwner, error) {
	rows, err := s.store.ListManagedOwnership(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]model.ManagedOwner, len(rows))
	for _, row := range rows {
		out[managedOwnerKey(row.Kind, row.Name)] = row
	}
	return out, nil
}

func applyManaged(dst *string, dstRepoID *int64, owners map[string]model.ManagedOwner, kind, name string) {
	if owner, ok := owners[managedOwnerKey(kind, name)]; ok {
		*dst = owner.ConfigRepoName
		*dstRepoID = owner.ConfigRepoID
		return
	}
	*dst = ""
	*dstRepoID = 0
}

func (s *Server) annotateApplications(ctx context.Context, apps []model.Application) error {
	if len(apps) == 0 {
		return nil
	}
	owners, err := s.managedOwners(ctx)
	if err != nil {
		return err
	}
	for i := range apps {
		applyManaged(&apps[i].ManagedBy, &apps[i].ManagedByRepoID, owners, gitops.KindApplication, apps[i].Name)
		// Inventory data can contain SSH credentials. It is served through the
		// dedicated inventory endpoint, which redacts secrets.
		apps[i].InventoryData = ""
	}
	return nil
}

func (s *Server) annotateApplication(ctx context.Context, app *model.Application) error {
	apps := []model.Application{*app}
	if err := s.annotateApplications(ctx, apps); err != nil {
		return err
	}
	*app = apps[0]
	return nil
}

func (s *Server) annotateProjects(ctx context.Context, projects []model.Project) error {
	if len(projects) == 0 {
		return nil
	}
	owners, err := s.managedOwners(ctx)
	if err != nil {
		return err
	}
	for i := range projects {
		applyManaged(&projects[i].ManagedBy, &projects[i].ManagedByRepoID, owners, gitops.KindProject, projects[i].Name)
	}
	return nil
}

func (s *Server) annotateProject(ctx context.Context, project *model.Project) error {
	projects := []model.Project{*project}
	if err := s.annotateProjects(ctx, projects); err != nil {
		return err
	}
	*project = projects[0]
	return nil
}

func (s *Server) annotateRepositories(ctx context.Context, repos []model.Repository) error {
	if len(repos) == 0 {
		return nil
	}
	owners, err := s.managedOwners(ctx)
	if err != nil {
		return err
	}
	for i := range repos {
		applyManaged(&repos[i].ManagedBy, &repos[i].ManagedByRepoID, owners, gitops.KindRepository, repoManagedKey(repos[i].Project, repos[i].URL))
	}
	return nil
}

func (s *Server) annotateRepository(ctx context.Context, repo *model.Repository) error {
	repos := []model.Repository{*repo}
	if err := s.annotateRepositories(ctx, repos); err != nil {
		return err
	}
	*repo = repos[0]
	return nil
}

func (s *Server) annotateAppStatuses(ctx context.Context, instanceID string, statuses []AppStatus) error {
	return s.annotateAppStatusMap(ctx, map[string][]AppStatus{instanceID: statuses})
}

func (s *Server) annotateAppStatusMap(ctx context.Context, byInstance map[string][]AppStatus) error {
	if len(byInstance) == 0 {
		return nil
	}
	owners, err := s.managedOwners(ctx)
	if err != nil {
		return err
	}
	for instanceID, statuses := range byInstance {
		for i := range statuses {
			app := &statuses[i].Application
			applyManaged(&app.ManagedBy, &app.ManagedByRepoID, owners, gitops.KindApplication, app.Name)
			applyManaged(&statuses[i].ManagedBy, &statuses[i].ManagedByRepoID, owners, gitops.KindAssignment, assignmentManagedKey(instanceID, app.Name))
		}
	}
	return nil
}
