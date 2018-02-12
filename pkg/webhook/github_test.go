package webhook

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-github/github"
	"gopkg.in/gin-gonic/gin.v1"

	"github.com/Azure/brigade/pkg/brigade"
	"github.com/Azure/brigade/pkg/storage"
)

type testStore struct {
	proj   *brigade.Project
	builds []*brigade.Build
	err    error
	storage.Store
}

func (s *testStore) GetProject(name string) (*brigade.Project, error) {
	return s.proj, s.err
}

func (s *testStore) CreateBuild(build *brigade.Build) error {
	s.builds = append(s.builds, build)
	return s.err
}

func newTestStore() *testStore {
	return &testStore{
		proj: &brigade.Project{
			Name:         "baxterthehacker/public-repo",
			SharedSecret: "asdf",
		},
	}
}

func newTestGithubHandler(store storage.Store, t *testing.T) *githubHook {
	s := NewGithubHook(store, false)
	s.getFile = func(commit, path string, proj *brigade.Project) ([]byte, error) {
		return []byte(""), nil
	}
	s.createStatus = func(commit string, proj *brigade.Project, status *github.RepoStatus) error {
		return nil
	}
	return s
}

func TestGithubHandler(t *testing.T) {

	tests := []struct {
		event        string
		commit       string
		payloadFile  string
		renamedEvent string
		mustFail     bool
	}{
		{
			event:       "push",
			commit:      "0d1a26e67d8f5eaf1f6ba5c57fc3c7d91ac0fd1c",
			payloadFile: "testdata/github-push-payload.json",
		},
		{
			event:       "push",
			commit:      "0d1a26e67d8f5eaf1f6ba5c57fc3c7d91ac0fd1c",
			payloadFile: "testdata/github-push-delete-branch.json",
			mustFail:    true,
		},
		{
			event:       "pull_request",
			commit:      "0d1a26e67d8f5eaf1f6ba5c57fc3c7d91ac0fd1c",
			payloadFile: "testdata/github-pull_request-payload.json",
		},
		{
			event:       "pull_request_review",
			commit:      "b7a1f9c27caa4e03c14a88feb56e2d4f7500aa63",
			payloadFile: "testdata/github-pull_request_review-payload.json",
		},
		{
			event:        "pull_request",
			commit:       "ad0703ac08e80448764b34dc089d0f73a1242ae9",
			payloadFile:  "testdata/github-pull_request-labeled-payload.json",
			renamedEvent: "pull_request:labeled",
		},
		{
			event:       "status",
			commit:      "9049f1265b7d61be4a8904a9a27120d2064dab3b",
			payloadFile: "testdata/github-status-payload.json",
		},
		{
			event:       "release",
			commit:      "0.0.1",
			payloadFile: "testdata/github-release-payload.json",
		},
		{
			event:       "create",
			commit:      "0.0.1",
			payloadFile: "testdata/github-create-payload.json",
		},
		{
			event:       "commit_comment",
			commit:      "9049f1265b7d61be4a8904a9a27120d2064dab3b",
			payloadFile: "testdata/github-commit_comment-payload.json",
		},
		{
			event:       "deployment",
			commit:      "9049f1265b7d61be4a8904a9a27120d2064dab3b",
			payloadFile: "testdata/github-deployment-payload.json",
		},
		{
			event:       "deployment_status",
			commit:      "9049f1265b7d61be4a8904a9a27120d2064dab3b",
			payloadFile: "testdata/github-deployment_status-payload.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.payloadFile, func(t *testing.T) {
			store := newTestStore()
			s := newTestGithubHandler(store, t)

			// TODO: do we want to test this?
			s.createStatus = func(commit string, proj *brigade.Project, status *github.RepoStatus) error {
				if status.GetState() != "pending" {
					t.Error("RepoStatus.State is not correct")
				}
				if status.GetDescription() != "Building" {
					t.Error("RepoStatus.Building is not correct")
				}
				if commit != tt.commit {
					t.Error("commit is not correct")
				}
				return nil
			}

			payload, err := ioutil.ReadFile(tt.payloadFile)
			if err != nil {
				t.Fatalf("failed to read testdata: %s", err)
			}

			w := httptest.NewRecorder()
			r, err := http.NewRequest("POST", "", bytes.NewReader(payload))
			if err != nil {
				t.Fatalf("failed to create request: %s", err)
			}
			r.Header.Add("X-GitHub-Event", tt.event)
			r.Header.Add("X-Hub-Signature", SHA1HMAC([]byte("asdf"), payload))

			ctx, _ := gin.CreateTestContext(w)
			ctx.Request = r

			s.Handle(ctx)

			if w.Code != http.StatusOK {
				t.Fatalf("unexpected error: %d\n%s", w.Code, w.Body.String())
			}

			if tt.mustFail {
				if len(store.builds) > 0 {
					t.Fatal("expected failed hook.")
				}
				return
			}
			if len(store.builds) != 1 {
				t.Fatal("expected a build created")
			}
			if ee := store.builds[0].Type; tt.renamedEvent != "" {
				if ee != tt.renamedEvent {
					t.Errorf("Build.Type is not correct. Expected renamed event %q, got %q", tt.renamedEvent, ee)
				}
			} else if ee != tt.event {
				t.Errorf("Build.Type is not correct. Expected event %q, got %q", tt.event, ee)
			}
			if store.builds[0].Provider != "github" {
				t.Error("Build.Provider is not correct")
			}
			if store.builds[0].Commit != tt.commit {
				t.Error("Build.Commit is not correct")
			}
		})
	}
}

func TestGithubHandler_ping(t *testing.T) {
	store := newTestStore()
	s := newTestGithubHandler(store, t)

	w := httptest.NewRecorder()
	r, err := http.NewRequest("POST", "", nil)
	if err != nil {
		t.Fatalf("failed to create request: %s", err)
	}
	r.Header.Add("X-GitHub-Event", "ping")

	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = r

	s.Handle(ctx)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected error: %d\n%s", w.Code, w.Body.String())
	}
}

func TestGithubHandler_badevent(t *testing.T) {
	store := newTestStore()
	s := newTestGithubHandler(store, t)

	w := httptest.NewRecorder()
	r, err := http.NewRequest("POST", "", nil)
	if err != nil {
		t.Fatalf("failed to create request: %s", err)
	}
	r.Header.Add("X-GitHub-Event", "funzone")

	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = r

	s.Handle(ctx)

	if w.Code != http.StatusOK {
		t.Fatalf("expected unsupported verb to return a 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Ignored") {
		t.Fatalf("unexpected body: %d\n%s", w.Code, w.Body.String())
	}
}

func TestGithubHandler_WithDefaultScript(t *testing.T) {
	store := newTestStore()
	store.proj.DefaultScript = `console.log("hello default script")'`
	s := newTestGithubHandler(store, t)
	// Treat the repo to have no file, to eventually trigger the fall-back to the default script
	s.getFile = failingFileGet

	payloadFile := "testdata/github-push-payload.json"
	event := "push"

	payload, err := ioutil.ReadFile(payloadFile)
	if err != nil {
		t.Fatalf("failed to read testdata: %s", err)
	}

	w := httptest.NewRecorder()
	r, err := http.NewRequest("POST", "", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("failed to create request: %s", err)
	}
	r.Header.Add("X-GitHub-Event", event)
	r.Header.Add("X-Hub-Signature", SHA1HMAC([]byte("asdf"), payload))

	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = r

	s.Handle(ctx)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected error: %d\n%s", w.Code, w.Body.String())
	}
	if len(store.builds) != 1 {
		t.Fatalf("expected exactly one build to be created, but there are %d", len(store.builds))
	}
	script := string(store.builds[0].Script)
	if script != store.proj.DefaultScript {
		t.Errorf("unexpected build script: %s", script)
	}
}

func TestTruncAt(t *testing.T) {
	if "foo" != truncAt("foo", 100) {
		t.Fatal("modified string that was fine.")
	}

	if got := truncAt("foobar", 6); got != "foobar" {
		t.Errorf("Unexpected truncation of foobar: %s", got)
	}

	if got := truncAt("foobar1", 6); got != "foo..." {
		t.Errorf("Unexpected truncation of foobar1: %s", got)
	}
}

// failingFileGet is a `fileGetter` which is useful for simulating a situation that the project repository to contain no file
func failingFileGet(commit, path string, proj *brigade.Project) ([]byte, error) {
	return []byte{}, fmt.Errorf("simulated \"missing file\" error for commit=%s, path=%s, proj.name=%s", commit, path, proj.Name)
}
