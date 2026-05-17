package docker

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"

	"github.com/HarkushaVlad/docker-monitor-bot/internal/utils"
)

const (
	LabelComposeProject    = "com.docker.compose.project"
	LabelComposeService    = "com.docker.compose.service"
	LabelComposeWorkingDir = "com.docker.compose.project.working_dir"
	LabelComposeConfigFile = "com.docker.compose.project.config_files"
)

type Service struct {
	client      *client.Client
	ignoreStore *logIgnoreStore
}

func NewService(ignoreRulesFile string) (*Service, error) {
	c, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %v", err)
	}

	ignoreStore, err := newLogIgnoreStore(ignoreRulesFile)
	if err != nil {
		return nil, fmt.Errorf("failed to init log ignore store: %v", err)
	}

	return &Service{client: c, ignoreStore: ignoreStore}, nil
}

func (s *Service) Client() *client.Client {
	return s.client
}

func (s *Service) ListLogIgnoreRules() []LogIgnoreRule {
	if s.ignoreStore == nil {
		return nil
	}
	return s.ignoreStore.List()
}

func (s *Service) AddLogIgnoreRule(scopeSpec, match string) (LogIgnoreRule, error) {
	if s.ignoreStore == nil {
		return LogIgnoreRule{}, fmt.Errorf("log ignore store is not initialized")
	}
	return s.ignoreStore.Add(scopeSpec, match)
}

func (s *Service) DeleteLogIgnoreRule(id int) (bool, error) {
	if s.ignoreStore == nil {
		return false, fmt.Errorf("log ignore store is not initialized")
	}
	return s.ignoreStore.Delete(id)
}

type ComposeProject struct {
	Name       string
	WorkingDir string
	ConfigFile string
	Services   []types.Container
}

func (p *ComposeProject) RunningCount() int {
	n := 0
	for _, c := range p.Services {
		if c.State == "running" {
			n++
		}
	}
	return n
}

type ContainerGroups struct {
	Projects   []ComposeProject
	Standalone []types.Container
}

func (s *Service) GetContainerGroups(ctx context.Context) (*ContainerGroups, error) {
	containers, err := s.client.ContainerList(ctx, types.ContainerListOptions{All: true})
	if err != nil {
		return nil, err
	}

	projectMap := make(map[string]*ComposeProject)
	var standalone []types.Container

	for _, c := range containers {
		if !utils.IsUserContainer(c) {
			continue
		}

		projectName := c.Labels[LabelComposeProject]
		if projectName != "" {
			proj, ok := projectMap[projectName]
			if !ok {
				proj = &ComposeProject{
					Name:       projectName,
					WorkingDir: c.Labels[LabelComposeWorkingDir],
					ConfigFile: c.Labels[LabelComposeConfigFile],
				}
				projectMap[projectName] = proj
			}
			proj.Services = append(proj.Services, c)
		} else {
			standalone = append(standalone, c)
		}
	}

	var projects []ComposeProject
	for _, p := range projectMap {
		sort.Slice(p.Services, func(i, j int) bool {
			return ServiceName(p.Services[i]) < ServiceName(p.Services[j])
		})
		projects = append(projects, *p)
	}
	sort.Slice(projects, func(i, j int) bool {
		return projects[i].Name < projects[j].Name
	})

	return &ContainerGroups{
		Projects:   projects,
		Standalone: standalone,
	}, nil
}

func (s *Service) ListUserContainers(ctx context.Context) ([]types.Container, error) {
	containers, err := s.client.ContainerList(ctx, types.ContainerListOptions{All: true})
	if err != nil {
		return nil, err
	}
	var result []types.Container
	for _, c := range containers {
		if utils.IsUserContainer(c) {
			result = append(result, c)
		}
	}
	return result, nil
}

func (s *Service) ContainerInspect(ctx context.Context, id string) (types.ContainerJSON, error) {
	return s.client.ContainerInspect(ctx, id)
}

func (s *Service) ContainerStart(ctx context.Context, id string) error {
	return s.client.ContainerStart(ctx, id, types.ContainerStartOptions{})
}

func (s *Service) ContainerStop(ctx context.Context, id string) error {
	timeout := 10 * time.Second
	return s.client.ContainerStop(ctx, id, &timeout)
}

func (s *Service) ContainerRestart(ctx context.Context, id string) error {
	timeout := 10 * time.Second
	return s.client.ContainerRestart(ctx, id, &timeout)
}

func (s *Service) composeExec(ctx context.Context, workingDir, configFile string, args ...string) error {
	cmdArgs := []string{"compose"}
	if configFile != "" {
		cmdArgs = append(cmdArgs, "-f", configFile)
	}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.CommandContext(ctx, "docker", cmdArgs...)
	if workingDir != "" {
		cmd.Dir = workingDir
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (s *Service) StartProject(ctx context.Context, workingDir, configFile string) error {
	return s.composeExec(ctx, workingDir, configFile, "start")
}

func (s *Service) StopProject(ctx context.Context, workingDir, configFile string) error {
	return s.composeExec(ctx, workingDir, configFile, "stop")
}

func (s *Service) RestartProject(ctx context.Context, workingDir, configFile string) error {
	return s.composeExec(ctx, workingDir, configFile, "restart")
}

func (s *Service) RebuildProject(ctx context.Context, workingDir, configFile string) error {
	return s.composeExec(ctx, workingDir, configFile, "up", "-d", "--build")
}

func ServiceName(c types.Container) string {
	if name := c.Labels[LabelComposeService]; name != "" {
		return name
	}
	return utils.ContainerName(c)
}

func IsComposeContainer(c types.Container) bool {
	return c.Labels[LabelComposeProject] != ""
}
