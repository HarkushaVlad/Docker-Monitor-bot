package docker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	logIgnoreScopeGlobal    = "global"
	logIgnoreScopeProject   = "project"
	logIgnoreScopeService   = "service"
	logIgnoreScopeContainer = "container"
)

type LogIgnoreRule struct {
	ID         int       `json:"id"`
	ScopeType  string    `json:"scope_type"`
	ScopeValue string    `json:"scope_value,omitempty"`
	Match      string    `json:"match"`
	CreatedAt  time.Time `json:"created_at"`
}

func (r LogIgnoreRule) ScopeSpec() string {
	switch r.ScopeType {
	case logIgnoreScopeGlobal:
		return "global"
	case logIgnoreScopeProject, logIgnoreScopeService, logIgnoreScopeContainer:
		return fmt.Sprintf("%s:%s", r.ScopeType, r.ScopeValue)
	default:
		return r.ScopeType
	}
}

type LogContext struct {
	ContainerName string
	ProjectName   string
	ServiceName   string
	DisplayName   string
}

type logIgnoreStore struct {
	mu     sync.RWMutex
	path   string
	nextID int
	rules  []LogIgnoreRule
}

func newLogIgnoreStore(path string) (*logIgnoreStore, error) {
	store := &logIgnoreStore{
		path:   path,
		nextID: 1,
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *logIgnoreStore) load() error {
	if s.path == "" {
		return fmt.Errorf("log ignore rules file path is required")
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read rules file: %w", err)
	}

	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}

	var rules []LogIgnoreRule
	if err := json.Unmarshal(data, &rules); err != nil {
		return fmt.Errorf("parse rules file: %w", err)
	}

	maxID := 0
	for _, rule := range rules {
		if rule.ID > maxID {
			maxID = rule.ID
		}
	}

	s.rules = rules
	s.nextID = maxID + 1
	if s.nextID == 0 {
		s.nextID = 1
	}

	return nil
}

func (s *logIgnoreStore) List() []LogIgnoreRule {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rules := make([]LogIgnoreRule, len(s.rules))
	copy(rules, s.rules)
	return rules
}

func (s *logIgnoreStore) Add(scopeSpec, match string) (LogIgnoreRule, error) {
	scopeType, scopeValue, err := parseLogIgnoreScope(scopeSpec)
	if err != nil {
		return LogIgnoreRule{}, err
	}

	match = strings.TrimSpace(match)
	if match == "" {
		return LogIgnoreRule{}, fmt.Errorf("match text is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, existing := range s.rules {
		if existing.ScopeType == scopeType && strings.EqualFold(existing.ScopeValue, scopeValue) && strings.EqualFold(existing.Match, match) {
			return existing, nil
		}
	}

	rule := LogIgnoreRule{
		ID:         s.nextID,
		ScopeType:  scopeType,
		ScopeValue: scopeValue,
		Match:      match,
		CreatedAt:  time.Now().UTC(),
	}

	previous := append([]LogIgnoreRule(nil), s.rules...)
	s.rules = append(s.rules, rule)
	s.nextID++

	if err := s.saveLocked(); err != nil {
		s.rules = previous
		s.nextID = rule.ID
		return LogIgnoreRule{}, err
	}

	return rule, nil
}

func (s *logIgnoreStore) Delete(id int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	index := -1
	for i, rule := range s.rules {
		if rule.ID == id {
			index = i
			break
		}
	}
	if index == -1 {
		return false, nil
	}

	previous := append([]LogIgnoreRule(nil), s.rules...)
	s.rules = append(s.rules[:index], s.rules[index+1:]...)
	if err := s.saveLocked(); err != nil {
		s.rules = previous
		return false, err
	}

	return true, nil
}

func (s *logIgnoreStore) ShouldIgnore(ctx LogContext, group []string) bool {
	if len(group) == 0 {
		return false
	}

	joined := strings.ToLower(strings.Join(group, "\n"))

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, rule := range s.rules {
		if !ruleMatchesContext(rule, ctx) {
			continue
		}
		if strings.Contains(joined, strings.ToLower(rule.Match)) {
			return true
		}
	}

	return false
}

func (s *logIgnoreStore) saveLocked() error {
	if dir := filepath.Dir(s.path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create rules directory: %w", err)
		}
	}

	data, err := json.MarshalIndent(s.rules, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal rules: %w", err)
	}
	data = append(data, '\n')

	tempPath := s.path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return fmt.Errorf("write temp rules file: %w", err)
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		return fmt.Errorf("replace rules file: %w", err)
	}

	return nil
}

func parseLogIgnoreScope(scopeSpec string) (string, string, error) {
	scopeSpec = strings.TrimSpace(scopeSpec)
	if scopeSpec == "" {
		return "", "", fmt.Errorf("scope is required")
	}

	if scopeSpec == "*" || strings.EqualFold(scopeSpec, logIgnoreScopeGlobal) {
		return logIgnoreScopeGlobal, "", nil
	}

	parts := strings.SplitN(scopeSpec, ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid scope %q", scopeSpec)
	}

	scopeType := strings.ToLower(strings.TrimSpace(parts[0]))
	scopeValue := strings.TrimSpace(parts[1])
	if scopeValue == "" {
		return "", "", fmt.Errorf("scope value is required")
	}

	switch scopeType {
	case logIgnoreScopeContainer, logIgnoreScopeProject:
		return scopeType, scopeValue, nil
	case logIgnoreScopeService:
		normalized, err := normalizeServiceScope(scopeValue)
		if err != nil {
			return "", "", err
		}
		return scopeType, normalized, nil
	default:
		return "", "", fmt.Errorf("unsupported scope type %q", scopeType)
	}
}

func normalizeServiceScope(value string) (string, error) {
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("service scope must look like project/service")
	}

	project := strings.TrimSpace(parts[0])
	service := strings.TrimSpace(parts[1])
	if project == "" || service == "" {
		return "", fmt.Errorf("service scope must include both project and service names")
	}

	return project + "/" + service, nil
}

func ruleMatchesContext(rule LogIgnoreRule, ctx LogContext) bool {
	switch rule.ScopeType {
	case logIgnoreScopeGlobal:
		return true
	case logIgnoreScopeProject:
		return strings.EqualFold(rule.ScopeValue, ctx.ProjectName)
	case logIgnoreScopeContainer:
		return strings.EqualFold(rule.ScopeValue, ctx.ContainerName)
	case logIgnoreScopeService:
		serviceKey := ctx.ProjectName + "/" + ctx.ServiceName
		return ctx.ProjectName != "" && ctx.ServiceName != "" && strings.EqualFold(rule.ScopeValue, serviceKey)
	default:
		return false
	}
}

func RuleMatchesContext(rule LogIgnoreRule, ctx LogContext) bool {
	return ruleMatchesContext(rule, ctx)
}
