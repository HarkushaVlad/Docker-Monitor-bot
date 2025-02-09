package docker

import (
	"fmt"

	"github.com/docker/docker/client"
)

var DockerClient *client.Client

func InitDockerClient() error {
	var err error
	DockerClient, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("failed to initialize Docker client: %v", err)
	}
	return nil
}
