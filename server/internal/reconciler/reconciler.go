// Package reconciler implements the optional GitOps controller. It polls
// enabled ConfigRepos and applies Helios YAML manifests to the normal store.
// It never disables the imperative API: manual edits still work, and pruning is
// opt-in per config repository.
package reconciler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/slim/helios/server/internal/gitops"
	"github.com/slim/helios/server/internal/model"
	"github.com/slim/helios/server/internal/projects"
	"github.com/slim/helios/server/internal/store"
)

const scanInterval = 10 * time.Second

type Reconciler struct {
	store    store.Store
	log      *slog.Logger
	stateDir string
	mu       sync.Mutex
}

func New(st store.Store, log *slog.Logger, stateDir string) *Reconciler {
	if stateDir == "" {
		stateDir = filepath.Join(os.TempDir(), "helios-gitops")
	}
	return &Reconciler{store: st, log: log, stateDir: stateDir}
}

// Start runs the polling loop until ctx is cancelled. With no config repos it
// is a harmless no-op, so the feature is opt-in by creating a ConfigRepo.
func (r *Reconciler) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(scanInterval)
		defer ticker.Stop()
		for {
			r.runDue(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (r *Reconciler) runDue(ctx context.Context) {
	repos, err := r.store.ListConfigRepos(ctx)
	if err != nil {
		r.log.Warn("gitops: list config repos failed", "error", err)
		return
	}
	for _, repo := range repos {
		if !repo.Enabled {
			continue
		}
		if !repo.LastSynced.IsZero() && time.Since(repo.LastSynced) < time.Duration(repo.PollInterval)*time.Second {
			continue
		}
		if err := r.Sync(ctx, repo.ID); err != nil {
			r.log.Warn("gitops: sync failed", "config_repo", repo.Name, "error", err)
		}
	}
}

// Sync reconciles one config repo immediately. It is safe to call from an API
// handler and from the polling loop; a single mutex serializes reconciles.
func (r *Reconciler) Sync(ctx context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	repo, err := r.store.GetConfigRepo(ctx, id)
	if err != nil {
		return err
	}
	summary := map[string]int{}
	now := time.Now().UTC()

	commit, reconcileErr := r.reconcile(ctx, repo, summary)
	if commit == "" {
		commit = repo.LastCommit
	}
	errMsg := ""
	if reconcileErr != nil {
		errMsg = reconcileErr.Error()
	}
	if err := r.store.UpdateConfigRepoStatus(ctx, repo.ID, now, commit, errMsg, summary); err != nil {
		r.log.Warn("gitops: update config repo status failed", "config_repo", repo.Name, "error", err)
	}
	if reconcileErr != nil {
		return reconcileErr
	}
	r.log.Info("gitops: reconciled", "config_repo", repo.Name, "commit", short(commit), "summary", summary)
	return nil
}

func (r *Reconciler) reconcile(ctx context.Context, repo model.ConfigRepo, summary map[string]int) (string, error) {
	dir := filepath.Join(r.stateDir, fmt.Sprintf("repo-%d", repo.ID))
	creds := r.credsFor(ctx, repo.URL)
	_, after, err := gitops.Sync(ctx, dir, repo.URL, repo.Branch, creds)
	if err != nil {
		return "", fmt.Errorf("sync %s: %w", repo.URL, err)
	}

	docs, err := gitops.ParseDir(dir, repo.Path)
	if err != nil {
		return after, fmt.Errorf("parse manifests: %w", err)
	}

	desired, err := buildDesired(docs)
	if err != nil {
		return after, err
	}
	owners, err := r.owners(ctx, repo.ID)
	if err != nil {
		return after, err
	}
	old, err := r.store.ListManagedResources(ctx, repo.ID)
	if err != nil {
		return after, err
	}

	var errs []string
	apply := func(kind, label, ownerName string, fn func() error) {
		key := kind + "/" + ownerName
		if owner, ok := owners[key]; ok && owner != repo.ID {
			errs = append(errs, fmt.Sprintf("%s is already managed by another config repo", key))
			summary["errors"]++
			return
		}
		if err := fn(); err != nil {
			errs = append(errs, fmt.Sprintf("%s/%s: %v", kind, label, err))
			summary["errors"]++
			return
		}
		if err := r.store.SetManagedResource(ctx, repo.ID, kind, ownerName); err != nil {
			errs = append(errs, fmt.Sprintf("%s/%s: record ownership: %v", kind, label, err))
			summary["errors"]++
		}
	}

	for name, spec := range desired.projects {
		name, spec := name, spec
		apply(gitops.KindProject, name, name, func() error { return r.applyProject(ctx, name, spec, summary) })
	}
	for name, spec := range desired.repositories {
		name, spec := name, spec
		apply(gitops.KindRepository, name, spec.URL, func() error { return r.applyRepository(ctx, spec, summary) })
	}
	for name, spec := range desired.applications {
		name, spec := name, spec
		apply(gitops.KindApplication, name, name, func() error {
			return r.applyApplication(ctx, name, spec, desired, summary)
		})
	}
	for key, spec := range desired.assignments {
		key, spec := key, spec
		apply(gitops.KindAssignment, spec.Name, key, func() error { return r.applyAssignment(ctx, spec, summary) })
	}

	if repo.Prune {
		r.prune(ctx, repo.ID, old, desired, summary, &errs)
	}

	if len(errs) > 0 {
		return after, errors.New(strings.Join(errs, "; "))
	}
	return after, nil
}

type desiredState struct {
	projects     map[string]gitops.ProjectSpec
	repositories map[string]gitops.RepositorySpec
	applications map[string]gitops.ApplicationSpec
	assignments  map[string]gitops.AssignmentSpec
}

func buildDesired(docs []gitops.Document) (desiredState, error) {
	out := desiredState{
		projects:     map[string]gitops.ProjectSpec{},
		repositories: map[string]gitops.RepositorySpec{},
		applications: map[string]gitops.ApplicationSpec{},
		assignments:  map[string]gitops.AssignmentSpec{},
	}
	seen := map[string]bool{}
	for _, doc := range docs {
		if err := doc.Validate(); err != nil {
			return out, fmt.Errorf("%s: %w", doc.Source, err)
		}
		key := doc.String()
		if seen[key] {
			return out, fmt.Errorf("%s: duplicate %s", doc.Source, key)
		}
		seen[key] = true
		switch doc.Kind {
		case gitops.KindProject:
			spec, err := doc.Project()
			if err != nil {
				return out, fmt.Errorf("%s: %w", doc.Source, err)
			}
			out.projects[doc.Metadata.Name] = spec
		case gitops.KindRepository:
			spec, err := doc.Repository()
			if err != nil {
				return out, fmt.Errorf("%s: %w", doc.Source, err)
			}
			out.repositories[doc.Metadata.Name] = spec
		case gitops.KindApplication:
			spec, err := doc.Application()
			if err != nil {
				return out, fmt.Errorf("%s: %w", doc.Source, err)
			}
			out.applications[doc.Metadata.Name] = spec
		case gitops.KindAssignment:
			spec, err := doc.Assignment()
			if err != nil {
				return out, fmt.Errorf("%s: %w", doc.Source, err)
			}
			key := assignmentKey(spec.Instance, spec.Application)
			if _, exists := out.assignments[key]; exists {
				return out, fmt.Errorf("%s: duplicate assignment %s", doc.Source, key)
			}
			out.assignments[key] = spec
		}
	}
	return out, nil
}

func (r *Reconciler) owners(ctx context.Context, current int64) (map[string]int64, error) {
	repos, err := r.store.ListConfigRepos(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, repo := range repos {
		if repo.ID == current {
			continue
		}
		items, err := r.store.ListManagedResources(ctx, repo.ID)
		if err != nil {
			return nil, err
		}
		for _, m := range items {
			out[m.Kind+"/"+m.Name] = repo.ID
		}
	}
	return out, nil
}

func (r *Reconciler) applyProject(ctx context.Context, name string, spec gitops.ProjectSpec, summary map[string]int) error {
	if !projects.ValidName(name) || name == projects.Global {
		return fmt.Errorf("invalid project name %q", name)
	}
	existing, err := r.store.GetProjectByName(ctx, name)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if _, err := r.store.CreateProject(ctx, model.Project{Name: name, RepoPatterns: spec.RepoPatterns}); err != nil {
			return err
		}
		summary["created"]++
		return nil
	}
	if existing.RepoPatterns == spec.RepoPatterns {
		summary["unchanged"]++
		return nil
	}
	existing.RepoPatterns = spec.RepoPatterns
	if err := r.store.UpdateProject(ctx, existing); err != nil {
		return err
	}
	summary["updated"]++
	return nil
}

func (r *Reconciler) applyRepository(ctx context.Context, spec gitops.RepositorySpec, summary map[string]int) error {
	if strings.TrimSpace(spec.URL) == "" {
		return errors.New("repository url is required")
	}
	existing, err := r.store.GetRepositoryByURL(ctx, spec.URL)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		_, err := r.store.CreateRepository(ctx, model.Repository{
			Name:               spec.Name,
			URL:                spec.URL,
			Username:           spec.Username,
			Password:           spec.Password,
			SSHKey:             spec.SSHKey,
			InsecureSkipVerify: spec.InsecureSkipVerify,
		})
		if err != nil {
			return err
		}
		summary["created"]++
		return nil
	}
	next := existing
	if spec.Name != "" {
		next.Name = spec.Name
	}
	if spec.Username != "" {
		next.Username = spec.Username
	}
	if spec.Password != "" {
		next.Password = spec.Password
	}
	if spec.SSHKey != "" {
		next.SSHKey = spec.SSHKey
	}
	next.InsecureSkipVerify = next.InsecureSkipVerify || spec.InsecureSkipVerify
	if next == existing {
		summary["unchanged"]++
		return nil
	}
	if err := r.store.UpdateRepository(ctx, next); err != nil {
		return err
	}
	summary["updated"]++
	return nil
}

func (r *Reconciler) applyApplication(ctx context.Context, name string, spec gitops.ApplicationSpec, desired desiredState, summary map[string]int) error {
	if spec.SyncInterval < 0 || (spec.SyncInterval > 0 && spec.SyncInterval < 10) {
		return errors.New("syncInterval must be 0 (inherit) or at least 10 seconds")
	}
	if spec.Project != projects.Global {
		if !projects.ValidName(spec.Project) {
			return fmt.Errorf("invalid project name %q", spec.Project)
		}
		if _, err := r.store.GetProjectByName(ctx, spec.Project); err != nil {
			return fmt.Errorf("unknown project %q", spec.Project)
		}
	}
	repoURL := spec.RepoURL
	if repoURL == "" && spec.RepoRef != "" {
		if ref, ok := desired.repositories[spec.RepoRef]; ok {
			repoURL = ref.URL
		} else if repos, err := r.store.ListRepositories(ctx); err == nil {
			for _, repo := range repos {
				if repo.Name == spec.RepoRef {
					repoURL = repo.URL
					break
				}
			}
		}
	}
	if repoURL == "" {
		return fmt.Errorf("repository reference %q not found", spec.RepoRef)
	}
	if spec.Project != projects.Global {
		p, err := r.store.GetProjectByName(ctx, spec.Project)
		if err != nil {
			return err
		}
		if !projects.MatchRepo(p.RepoPatterns, repoURL) {
			return fmt.Errorf("repository %s is not allowed by project %s", repoURL, spec.Project)
		}
	}
	if _, err := r.store.EnsureRepositoryByURL(ctx, repoURL); err != nil {
		return err
	}

	existing, err := r.store.GetApplicationByName(ctx, name)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		_, err := r.store.CreateApplication(ctx, model.Application{
			Name:         name,
			RepoURL:      repoURL,
			Branch:       spec.Branch,
			Path:         spec.Path,
			Vars:         spec.Vars,
			AutoHeal:     spec.AutoHeal,
			SyncInterval: spec.SyncInterval,
			IgnorePaths:  spec.IgnorePaths,
			Project:      spec.Project,
		})
		if err != nil {
			return err
		}
		summary["created"]++
		return nil
	}
	next := existing
	next.Name = name
	next.RepoURL = repoURL
	next.Branch = spec.Branch
	next.Path = spec.Path
	next.Vars = spec.Vars
	next.AutoHeal = spec.AutoHeal
	next.SyncInterval = spec.SyncInterval
	next.IgnorePaths = spec.IgnorePaths
	next.Project = spec.Project
	if next == existing {
		summary["unchanged"]++
		return nil
	}
	if err := r.store.UpdateApplication(ctx, next); err != nil {
		return err
	}
	summary["updated"]++
	return nil
}

func (r *Reconciler) applyAssignment(ctx context.Context, spec gitops.AssignmentSpec, summary map[string]int) error {
	app, err := r.store.GetApplicationByName(ctx, spec.Application)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("unknown application %q", spec.Application)
		}
		return err
	}
	inst, err := r.store.GetInstance(ctx, spec.Instance)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("unknown instance %q", spec.Instance)
		}
		return err
	}
	if app.Project != inst.Project {
		return fmt.Errorf("application %q and instance %q belong to different projects", app.Name, inst.ID)
	}
	if spec.Project != "" && spec.Project != app.Project {
		return fmt.Errorf("assignment project %q does not match application project %q", spec.Project, app.Project)
	}

	_, err = r.store.GetAssignment(ctx, inst.ID, app.ID)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if err := r.store.Assign(ctx, inst.ID, app.ID); err != nil {
			return err
		}
		summary["created"]++
	} else {
		summary["unchanged"]++
	}
	if spec.AutoHeal != nil {
		if *spec.AutoHeal {
			return r.store.SetAssignmentAutoHeal(ctx, inst.ID, app.ID, true)
		}
		return r.store.ClearAssignmentAutoHeal(ctx, inst.ID, app.ID)
	}
	return nil
}

func (r *Reconciler) prune(ctx context.Context, repoID int64, old []model.ManagedResource, desired desiredState, summary map[string]int, errs *[]string) {
	// Prune dependants before their parents.
	order := map[string]int{
		gitops.KindAssignment:  0,
		gitops.KindApplication: 1,
		gitops.KindRepository:  2,
		gitops.KindProject:     3,
	}
	for i := 0; i < len(old); i++ {
		for j := i + 1; j < len(old); j++ {
			if order[old[j].Kind] < order[old[i].Kind] {
				old[i], old[j] = old[j], old[i]
			}
		}
	}
	for _, m := range old {
		if managedDesired(desired, m) {
			continue
		}
		if err := r.deleteManaged(ctx, m); err != nil {
			*errs = append(*errs, fmt.Sprintf("%s/%s: prune: %v", m.Kind, m.Name, err))
			summary["errors"]++
			continue
		}
		if err := r.store.DeleteManagedResource(ctx, repoID, m.Kind, m.Name); err != nil {
			*errs = append(*errs, fmt.Sprintf("%s/%s: prune ownership: %v", m.Kind, m.Name, err))
			summary["errors"]++
			continue
		}
		summary["deleted"]++
	}
}

func managedDesired(d desiredState, m model.ManagedResource) bool {
	switch m.Kind {
	case gitops.KindProject:
		_, ok := d.projects[m.Name]
		return ok
	case gitops.KindRepository:
		for _, spec := range d.repositories {
			if spec.URL == m.Name {
				return true
			}
		}
		return false
	case gitops.KindApplication:
		_, ok := d.applications[m.Name]
		return ok
	case gitops.KindAssignment:
		_, ok := d.assignments[m.Name]
		return ok
	}
	return true
}

func (r *Reconciler) deleteManaged(ctx context.Context, m model.ManagedResource) error {
	switch m.Kind {
	case gitops.KindAssignment:
		appName, instanceID, ok := splitAssignmentKey(m.Name)
		if !ok {
			return nil
		}
		app, err := r.store.GetApplicationByName(ctx, appName)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		return r.store.Unassign(ctx, instanceID, app.ID)
	case gitops.KindApplication:
		app, err := r.store.GetApplicationByName(ctx, m.Name)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		assignments, err := r.store.AssignmentsForApp(ctx, app.ID)
		if err != nil {
			return err
		}
		if len(assignments) > 0 {
			return fmt.Errorf("application is still assigned to %d instance(s)", len(assignments))
		}
		return r.store.DeleteApplication(ctx, app.ID)
	case gitops.KindRepository:
		repo, err := r.store.GetRepositoryByURL(ctx, m.Name)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		n, err := r.store.CountApplicationsByRepoURL(ctx, repo.URL)
		if err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("repository is still used by %d application(s)", n)
		}
		return r.store.DeleteRepository(ctx, repo.ID)
	case gitops.KindProject:
		p, err := r.store.GetProjectByName(ctx, m.Name)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		apps, err := r.store.CountApplicationsInProject(ctx, p.Name)
		if err != nil {
			return err
		}
		instances, err := r.store.CountInstancesInProject(ctx, p.Name)
		if err != nil {
			return err
		}
		if apps > 0 || instances > 0 {
			return fmt.Errorf("project is still in use by %d application(s) and %d instance(s)", apps, instances)
		}
		return r.store.DeleteProject(ctx, p.ID)
	}
	return nil
}

func (r *Reconciler) credsFor(ctx context.Context, url string) gitops.Creds {
	repo, err := r.store.GetRepositoryByURL(ctx, url)
	if err != nil {
		return gitops.Creds{}
	}
	return gitops.Creds{
		Username:           repo.Username,
		Password:           repo.Password,
		SSHKey:             repo.SSHKey,
		InsecureSkipVerify: repo.InsecureSkipVerify,
	}
}

func assignmentKey(instance, app string) string { return instance + "::" + app }

func splitAssignmentKey(key string) (app, instance string, ok bool) {
	parts := strings.SplitN(key, "::", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[1], parts[0], true
}

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
