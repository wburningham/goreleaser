package release

import (
	"os"

	"github.com/apex/log"

	"github.com/goreleaser/goreleaser/internal/artifact"
	"github.com/goreleaser/goreleaser/internal/client"
	"github.com/goreleaser/goreleaser/internal/pipe"
	"github.com/goreleaser/goreleaser/internal/semerrgroup"
	"github.com/goreleaser/goreleaser/pkg/context"
)

const (
	// ArtifactDownloadPath defines a string used to index into an artifact.Artifact's Extra k/v store to store a download path.
	ArtifactDownloadPath = "release_artifact_download_path"
)

// Pipe for release
type Pipe struct{}

func (Pipe) String() string {
	return "releasing"
}

// Default sets the pipe defaults
func (Pipe) Default(ctx *context.Context) error {
	if ctx.Config.Release.Disable {
		return nil
	}
	if ctx.Config.Release.NameTemplate == "" {
		ctx.Config.Release.NameTemplate = "{{.Tag}}"
	}
	if ctx.Config.Release.Repo.Name != "" {
		return nil
	}
	repo, err := remoteRepo()
	if err != nil && !ctx.Snapshot {
		return err
	}
	ctx.Config.Release.Repo = repo
	return nil
}

// Run the pipe
func (Pipe) Run(ctx *context.Context) error {
	c, err := client.New(ctx)
	if err != nil {
		return err
	}
	return doRun(ctx, c)
}

func doRun(ctx *context.Context, c client.Client) error {
	if ctx.Config.Release.Disable {
		return pipe.Skip("release pipe is disabled")
	}
	if ctx.SkipPublish {
		return pipe.ErrSkipPublishEnabled
	}
	log.WithField("tag", ctx.Git.CurrentTag).
		WithField("repo", ctx.Config.Release.Repo.String()).
		Info("creating or updating release")
	body, err := describeBody(ctx)
	if err != nil {
		return err
	}
	releaseID, err := c.CreateRelease(ctx, body.String())
	if err != nil {
		return err
	}
	var g = semerrgroup.New(ctx.Parallelism)
	for _, artifact := range ctx.Artifacts.Filter(
		artifact.Or(
			artifact.ByType(artifact.UploadableArchive),
			artifact.ByType(artifact.UploadableBinary),
			artifact.ByType(artifact.Checksum),
			artifact.ByType(artifact.Signature),
			artifact.ByType(artifact.LinuxPackage),
		),
	).List() {
		artifact := artifact
		g.Go(func() error {
			return upload(ctx, c, releaseID, artifact)
		})
	}
	return g.Wait()
}

func upload(ctx *context.Context, c client.Client, releaseID string, artifact artifact.Artifact) error {
	file, err := os.Open(artifact.Path)
	if err != nil {
		return err
	}
	defer file.Close() // nolint: errcheck
	log.WithField("file", file.Name()).WithField("name", artifact.Name).Infof("uploading to %s", ctx.StorageType)
	path, err := c.Upload(ctx, releaseID, artifact.Name, file)
	if err != nil {
		return err
	}
	return ctx.Artifacts.SetExtra(artifact.ID(), ArtifactDownloadPath, path)
}
