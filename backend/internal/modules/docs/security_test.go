package docs

import (
	"context"
	"github.com/zubans/tsfera/backend/pkg/platform"
	"testing"
	"time"
)

func TestReviewIndexMustCheckCurrentVisibility(t *testing.T) {
	repo := newMemDocsRepo(time.Now)
	repo.docs[1] = &Document{ID: 1, Visibility: VisibilityAdmins}
	src := &docIndexSource{repo: repo}
	auth := &platform.AuthContext{UserID: 2, Permissions: map[string]bool{"docs.view": true}}
	allowed, err := src.CanView(context.Background(), auth, "1", map[string]string{"visibility": "internal"})
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("restricted document remains visible through stale index metadata")
	}
}

func TestHiddenDocumentCommentsCannotBeChanged(t *testing.T) {
	repo := newMemDocsRepo(time.Now)
	repo.docs[1] = &Document{ID: 1, Visibility: VisibilityAdmins}
	repo.cmts[2] = &DocComment{ID: 2, DocumentID: 1, Body: "secret"}
	svc := NewService(repo, nil, nil, t.TempDir(), nil)
	auth := &platform.AuthContext{UserID: 2, Permissions: map[string]bool{"docs.view": true, "docs.comment": true}}
	if _, err := svc.ResolveComment(context.Background(), auth, 1, 2, true); err == nil {
		t.Fatal("resolved hidden comment")
	}
	if err := svc.DeleteComment(context.Background(), auth, 1, 2); err == nil {
		t.Fatal("deleted hidden comment")
	}
	if repo.cmts[2] == nil || repo.cmts[2].Resolved {
		t.Fatal("hidden comment changed")
	}
}

func TestDeletedDocumentIsHiddenFromIndex(t *testing.T) {
	repo := newMemDocsRepo(time.Now)
	repo.docs[1] = &Document{ID: 1, Visibility: VisibilityInternal, Deleted: true}
	src := &docIndexSource{repo: repo}
	allowed, err := src.CanView(context.Background(), &platform.AuthContext{IsSuperAdmin: true}, "1", map[string]string{"visibility": "internal"})
	if err != nil || allowed {
		t.Fatalf("deleted document visible: allowed=%v err=%v", allowed, err)
	}
}
