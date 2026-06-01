package docker

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/volume"
	dockerclient "github.com/docker/docker/client"
)

// Client wraps the Docker SDK client with higher-level convenience methods.
type Client struct {
	inner *dockerclient.Client
}

// New creates a Docker client connecting to the local Docker socket.
// Respects DOCKER_HOST env var; defaults to unix:///var/run/docker.sock.
func New() (*Client, error) {
	inner, err := dockerclient.NewClientWithOpts(
		dockerclient.FromEnv,
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return &Client{inner: inner}, nil
}

func (c *Client) Close() error {
	return c.inner.Close()
}

func (c *Client) Ping(ctx context.Context) error {
	_, err := c.inner.Ping(ctx)
	return err
}

func (c *Client) InspectByID(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	return c.inner.ContainerInspect(ctx, containerID)
}

func (c *Client) ListAllContainers(ctx context.Context) ([]types.Container, error) {
	return c.inner.ContainerList(ctx, container.ListOptions{All: true})
}

func (c *Client) ListRunningContainers(ctx context.Context) ([]types.Container, error) {
	return c.inner.ContainerList(ctx, container.ListOptions{})
}

func (c *Client) ServerVersion(ctx context.Context) (types.Version, error) {
	return c.inner.ServerVersion(ctx)
}

func (c *Client) ListImages(ctx context.Context) ([]image.Summary, error) {
	return c.inner.ImageList(ctx, image.ListOptions{All: true})
}

func (c *Client) ListDanglingImages(ctx context.Context) ([]image.Summary, error) {
	f := filters.NewArgs()
	f.Add("dangling", "true")
	return c.inner.ImageList(ctx, image.ListOptions{Filters: f})
}

func (c *Client) ListVolumes(ctx context.Context) (volume.ListResponse, error) {
	return c.inner.VolumeList(ctx, volume.ListOptions{})
}
