package bot

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/HarkushaVlad/docker-monitor-bot/internal/docker"
)

type viewType string

const (
	viewMain      viewType = "main"
	viewProject   viewType = "project"
	viewContainer viewType = "container"
)

type State struct {
	mu            sync.Mutex
	LastMessageID int
	View          viewType
	ProjectName   string
	ContainerID   string
	ShortIDMap    map[string]string
	ProjectMap    map[int]string
}

func (b *Bot) handleCommand(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	state := b.getState(chatID)
	state.mu.Lock()
	defer state.mu.Unlock()

	switch msg.Command() {
	case "start":
		b.cmdStart(chatID, state)
	case "check":
		b.cmdCheck(chatID, state)
	case "list":
		if state.LastMessageID != 0 {
			b.notifier.DeleteMessage(chatID, state.LastMessageID)
		}
		state.LastMessageID = 0
		state.View = viewMain
		b.showMainList(chatID, state)
	}
}

func (b *Bot) handleCallback(query *tgbotapi.CallbackQuery) {
	chatID := query.Message.Chat.ID
	data := query.Data
	state := b.getState(chatID)
	state.mu.Lock()
	defer state.mu.Unlock()

	b.notifier.AnswerCallbackQuery(query.ID, "")

	switch {
	case data == "back":
		b.handleBack(chatID, state)
	case data == "refresh":
		b.refreshView(chatID, state)

	case strings.HasPrefix(data, "proj:"):
		idx := 0
		fmt.Sscanf(strings.TrimPrefix(data, "proj:"), "%d", &idx)
		if name, ok := state.ProjectMap[idx]; ok {
			state.ProjectName = name
			state.View = viewProject
			b.showProjectView(chatID, state)
		}

	case strings.HasPrefix(data, "cnt:"):
		shortID := strings.TrimPrefix(data, "cnt:")
		state.View = viewContainer
		b.showContainerDetail(chatID, shortID, state)

	case strings.HasPrefix(data, "act:"):
		b.handleContainerAction(chatID, data, state)

	case strings.HasPrefix(data, "pact:"):
		b.handleProjectAction(chatID, data, state)
	}
}

// --- Commands ---

func (b *Bot) cmdStart(chatID int64, state *State) {
	text := "👋 <b>Docker Monitor</b>\n\n" +
		"<b>Commands:</b>\n" +
		"/check — quick status overview\n" +
		"/list — interactive container management"

	if state.LastMessageID != 0 {
		b.notifier.DeleteMessage(chatID, state.LastMessageID)
	}
	state.LastMessageID = b.notifier.SendText(chatID, text)
}

func (b *Bot) cmdCheck(chatID int64, state *State) {
	ctx := context.Background()
	groups, err := b.Docker.GetContainerGroups(ctx)
	if err != nil {
		b.sendOrEdit(chatID, state, fmt.Sprintf("❌ Failed to fetch containers: %v", err))
		return
	}

	if len(groups.Projects) == 0 && len(groups.Standalone) == 0 {
		b.sendOrEdit(chatID, state, "🔍 <b>No containers found</b>")
		return
	}

	var sb strings.Builder
	sb.WriteString("📊 <b>Docker Status</b>\n")

	for _, proj := range groups.Projects {
		running := proj.RunningCount()
		total := len(proj.Services)
		sb.WriteString(fmt.Sprintf("\n🗂 <b>%s</b> (%d/%d running)\n", proj.Name, running, total))
		for _, svc := range proj.Services {
			sb.WriteString(fmt.Sprintf("  %s %s — <code>%s</code>\n",
				statusIcon(svc.State),
				docker.ServiceName(svc),
				svc.Image,
			))
		}
	}

	if len(groups.Standalone) > 0 {
		sb.WriteString("\n📦 <b>Standalone</b>\n")
		for _, c := range groups.Standalone {
			sb.WriteString(fmt.Sprintf("  %s %s — <code>%s</code>\n",
				statusIcon(c.State),
				docker.ServiceName(c),
				c.Image,
			))
		}
	}

	b.sendOrEdit(chatID, state, sb.String())
}

// --- Views ---

func (b *Bot) showMainList(chatID int64, state *State) {
	ctx := context.Background()
	groups, err := b.Docker.GetContainerGroups(ctx)
	if err != nil {
		b.sendOrEditError(chatID, state, "Failed to fetch containers")
		return
	}

	if len(groups.Projects) == 0 && len(groups.Standalone) == 0 {
		b.sendOrEdit(chatID, state, "🔍 <b>No containers found</b>")
		return
	}

	state.ProjectMap = make(map[int]string)
	state.ShortIDMap = make(map[string]string)

	var rows [][]tgbotapi.InlineKeyboardButton

	if len(groups.Projects) > 0 {
		var projButtons []tgbotapi.InlineKeyboardButton
		for i, proj := range groups.Projects {
			state.ProjectMap[i] = proj.Name
			running := proj.RunningCount()
			total := len(proj.Services)

			icon := "🟢"
			if running == 0 {
				icon = "🔴"
			} else if running < total {
				icon = "🟡"
			}

			label := fmt.Sprintf("%s %s (%d/%d)", icon, proj.Name, running, total)
			projButtons = append(projButtons, tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("proj:%d", i)))
		}

		for _, btn := range projButtons {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(btn))
		}
	}

	if len(groups.Standalone) > 0 {
		var cntButtons []tgbotapi.InlineKeyboardButton
		for _, c := range groups.Standalone {
			shortID := c.ID[:12]
			state.ShortIDMap[shortID] = c.ID
			label := fmt.Sprintf("%s %s", statusIcon(c.State), docker.ServiceName(c))
			cntButtons = append(cntButtons, tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("cnt:%s", shortID)))
		}
		for i := 0; i < len(cntButtons); i += 2 {
			if i+1 < len(cntButtons) {
				rows = append(rows, tgbotapi.NewInlineKeyboardRow(cntButtons[i], cntButtons[i+1]))
			} else {
				rows = append(rows, tgbotapi.NewInlineKeyboardRow(cntButtons[i]))
			}
		}
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🔄 Refresh", "refresh"),
	))

	keyboard := tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}

	totalProjects := len(groups.Projects)
	totalStandalone := len(groups.Standalone)
	msgText := fmt.Sprintf("📦 <b>Docker Services</b>\n\n🗂 Compose projects: %d\n📦 Standalone: %d",
		totalProjects, totalStandalone)

	if state.LastMessageID == 0 {
		state.LastMessageID = b.notifier.SendTextWithKeyboard(chatID, msgText, keyboard)
	} else {
		b.notifier.EditMessageWithKeyboard(chatID, state.LastMessageID, msgText, keyboard)
	}
}

func (b *Bot) showProjectView(chatID int64, state *State) {
	ctx := context.Background()
	groups, err := b.Docker.GetContainerGroups(ctx)
	if err != nil {
		b.sendOrEditError(chatID, state, "Failed to fetch containers")
		return
	}

	var proj *docker.ComposeProject
	for i := range groups.Projects {
		if groups.Projects[i].Name == state.ProjectName {
			proj = &groups.Projects[i]
			break
		}
	}
	if proj == nil {
		b.sendOrEditError(chatID, state, "Project not found")
		return
	}

	state.ShortIDMap = make(map[string]string)

	var rows [][]tgbotapi.InlineKeyboardButton

	for _, svc := range proj.Services {
		shortID := svc.ID[:12]
		state.ShortIDMap[shortID] = svc.ID
		label := fmt.Sprintf("%s %s", statusIcon(svc.State), docker.ServiceName(svc))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("cnt:%s", shortID)),
		))
	}

	rows = append(rows,
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("▶️ Start All", "pact:start"),
			tgbotapi.NewInlineKeyboardButtonData("⏹ Stop All", "pact:stop"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Restart All", "pact:restart"),
			tgbotapi.NewInlineKeyboardButtonData("🔨 Rebuild", "pact:rebuild"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("↩️ Back", "back"),
		),
	)

	keyboard := tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}

	running := proj.RunningCount()
	total := len(proj.Services)
	msgText := fmt.Sprintf("🗂 <b>%s</b>\n\nServices: %d | Running: %d/%d",
		proj.Name, total, running, total)

	if state.LastMessageID == 0 {
		state.LastMessageID = b.notifier.SendTextWithKeyboard(chatID, msgText, keyboard)
	} else {
		b.notifier.EditMessageWithKeyboard(chatID, state.LastMessageID, msgText, keyboard)
	}
}

func (b *Bot) showContainerDetail(chatID int64, shortID string, state *State) {
	fullID, ok := state.ShortIDMap[shortID]
	if !ok {
		b.sendOrEditError(chatID, state, "Container not found")
		return
	}

	ctx := context.Background()
	info, err := b.Docker.ContainerInspect(ctx, fullID)
	if err != nil {
		b.sendOrEditError(chatID, state, "Container not found")
		return
	}

	state.ContainerID = fullID

	status := "🔴 Stopped"
	if info.State.Running {
		status = "🟢 Running"
	}

	createdTime, err := time.Parse(time.RFC3339Nano, info.Created)
	if err != nil {
		log.Printf("Error parsing creation time: %v", err)
		createdTime = time.Now()
	}

	name := strings.TrimPrefix(info.Name, "/")

	var sb strings.Builder
	sb.WriteString("<pre>")
	sb.WriteString(fmt.Sprintf("┌ Name:    %s\n", name))
	sb.WriteString(fmt.Sprintf("├ Status:  %s\n", status))
	sb.WriteString(fmt.Sprintf("├ Image:   %s\n", info.Config.Image))

	if project := info.Config.Labels[docker.LabelComposeProject]; project != "" {
		service := info.Config.Labels[docker.LabelComposeService]
		sb.WriteString(fmt.Sprintf("├ Project: %s\n", project))
		sb.WriteString(fmt.Sprintf("├ Service: %s\n", service))
	}

	sb.WriteString(fmt.Sprintf("└ Created: %s", createdTime.Format("2006-01-02 15:04:05")))
	sb.WriteString("</pre>")

	rows := [][]tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("▶️ Start", fmt.Sprintf("act:start:%s", shortID)),
			tgbotapi.NewInlineKeyboardButtonData("⏹ Stop", fmt.Sprintf("act:stop:%s", shortID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Restart", fmt.Sprintf("act:restart:%s", shortID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("↩️ Back", "back"),
		),
	}

	keyboard := tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}

	if state.LastMessageID == 0 {
		state.LastMessageID = b.notifier.SendTextWithKeyboard(chatID, sb.String(), keyboard)
	} else {
		b.notifier.EditMessageWithKeyboard(chatID, state.LastMessageID, sb.String(), keyboard)
	}
}

// --- Actions ---

func (b *Bot) handleContainerAction(chatID int64, data string, state *State) {
	parts := strings.SplitN(data, ":", 3)
	if len(parts) < 3 {
		return
	}
	action := parts[1]
	shortID := parts[2]

	fullID, ok := state.ShortIDMap[shortID]
	if !ok {
		b.sendOrEditError(chatID, state, "Container not found")
		return
	}

	ctx := context.Background()
	var err error

	switch action {
	case "start":
		err = b.Docker.ContainerStart(ctx, fullID)
	case "stop":
		err = b.Docker.ContainerStop(ctx, fullID)
	case "restart":
		err = b.Docker.ContainerRestart(ctx, fullID)
	default:
		return
	}

	if err != nil {
		b.sendOrEditError(chatID, state, fmt.Sprintf("Failed to %s: %v", action, err))
		return
	}

	time.Sleep(500 * time.Millisecond)

	b.showContainerDetail(chatID, shortID, state)
}

func (b *Bot) findProject(ctx context.Context, name string) (*docker.ComposeProject, error) {
	groups, err := b.Docker.GetContainerGroups(ctx)
	if err != nil {
		return nil, err
	}
	for i := range groups.Projects {
		if groups.Projects[i].Name == name {
			return &groups.Projects[i], nil
		}
	}
	return nil, fmt.Errorf("project %q not found", name)
}

func (b *Bot) handleProjectAction(chatID int64, data string, state *State) {
	action := strings.TrimPrefix(data, "pact:")
	projectName := state.ProjectName

	if projectName == "" {
		b.sendOrEditError(chatID, state, "No project selected")
		return
	}

	ctx := context.Background()

	proj, err := b.findProject(ctx, projectName)
	if err != nil {
		b.sendOrEditError(chatID, state, fmt.Sprintf("Failed to find project: %v", err))
		return
	}

	verb := actionVerb(action)
	b.notifier.EditMessageText(chatID, state.LastMessageID,
		fmt.Sprintf("⏳ <b>%s %s...</b>", verb, projectName))

	switch action {
	case "start":
		err = b.Docker.StartProject(ctx, proj.WorkingDir, proj.ConfigFile)
	case "stop":
		err = b.Docker.StopProject(ctx, proj.WorkingDir, proj.ConfigFile)
	case "restart":
		err = b.Docker.RestartProject(ctx, proj.WorkingDir, proj.ConfigFile)
	case "rebuild":
		err = b.Docker.RebuildProject(ctx, proj.WorkingDir, proj.ConfigFile)
	default:
		return
	}

	if err != nil {
		b.sendOrEditError(chatID, state, fmt.Sprintf("Failed to %s project: %v", action, err))
		return
	}

	time.Sleep(time.Second)

	b.showProjectView(chatID, state)
}

// --- Navigation ---

func (b *Bot) handleBack(chatID int64, state *State) {
	switch state.View {
	case viewContainer:
		if state.ProjectName != "" {
			state.View = viewProject
			b.showProjectView(chatID, state)
		} else {
			state.View = viewMain
			b.showMainList(chatID, state)
		}
	case viewProject:
		state.View = viewMain
		state.ProjectName = ""
		b.showMainList(chatID, state)
	default:
		state.View = viewMain
		b.showMainList(chatID, state)
	}
}

func (b *Bot) refreshView(chatID int64, state *State) {
	switch state.View {
	case viewProject:
		b.showProjectView(chatID, state)
	default:
		b.showMainList(chatID, state)
	}
}

// --- Helpers ---

func (b *Bot) sendOrEdit(chatID int64, state *State, text string) {
	if state.LastMessageID > 0 {
		b.notifier.EditMessageText(chatID, state.LastMessageID, text)
	} else {
		state.LastMessageID = b.notifier.SendText(chatID, text)
	}
}

func (b *Bot) sendOrEditError(chatID int64, state *State, text string) {
	b.sendOrEdit(chatID, state, "❌ "+text)
}

func statusIcon(state string) string {
	if state == "running" {
		return "🟢"
	}
	return "🔴"
}

func actionVerb(action string) string {
	switch action {
	case "start":
		return "Starting"
	case "stop":
		return "Stopping"
	case "restart":
		return "Restarting"
	default:
		s := action + "ing"
		if len(s) > 0 {
			return strings.ToUpper(s[:1]) + s[1:]
		}
		return s
	}
}
