package webhook

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	"gopkg.in/gin-gonic/gin.v1"

	"github.com/Azure/brigade/pkg/brigade"
	"github.com/Azure/brigade/pkg/storage"
)

type dockerPushHook struct {
	store   storage.Store
	getFile fileGetter
}

// NewDockerPushHook creates a new Docker Push handler for webhooks.
func NewDockerPushHook(s storage.Store) *dockerPushHook {
	return &dockerPushHook{
		store:   s,
		getFile: getFileFromGithub,
	}
}

// Handle handles a Push webhook event from DockerHub or a compatible agent.
func (s *dockerPushHook) Handle(c *gin.Context) {
	var pname, commit string
	orgName := c.Param("org")
	projName := c.Param("repo")
	log.Println(projName)
	if projName != "" {
		pname = fmt.Sprintf("%s/%s", orgName, projName)
	} else {
		pname = orgName
	}
	if commit = c.Query("commit"); commit == "" {
		commit = c.Param("commit")
	}
	log.Printf("Fetching commit %s for %s", commit, pname)

	body, err := ioutil.ReadAll(c.Request.Body)
	if err != nil {
		log.Printf("Failed to read body: %s", err)
		c.JSON(http.StatusBadRequest, gin.H{"status": "Malformed body"})
		return
	}
	defer c.Request.Body.Close()

	proj, err := s.store.GetProject(pname)
	if err != nil {
		log.Printf("Project %q not found. No secret loaded. %s", pname, err)
		c.JSON(http.StatusBadRequest, gin.H{"status": "project not found"})
		return
	}

	// Guard to make sure empty URL isn't sent to GitHub
	if proj.Repo.Name == "" {
		log.Printf("No Repo.Name on project")
		c.JSON(http.StatusBadRequest, gin.H{"status": "brigadejs not found"})
		return
	}

	go s.notifyDockerImagePush(proj, commit, body)
	c.JSON(200, gin.H{"status": "Success"})
}

func (s *dockerPushHook) notifyDockerImagePush(proj *brigade.Project, commit string, payload []byte) {
	if err := s.doDockerImagePush(proj, commit, payload); err != nil {
		log.Printf("failed dockerimagepush event: %s", err)
	}

}

func (s *dockerPushHook) doDockerImagePush(proj *brigade.Project, commit string, payload []byte) error {
	b := &brigade.Build{
		ProjectID: proj.ID,
		Type:      "image_push",
		Provider:  "dockerhub",
		Commit:    commit,
		Payload:   payload,
	}
	if proj.DefaultScript != "" {
		b.Script = []byte(proj.DefaultScript)
	}
	return s.store.CreateBuild(b)
}
