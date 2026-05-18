package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/HarkushaVlad/docker-monitor-bot/internal/docker"
	"github.com/HarkushaVlad/docker-monitor-bot/internal/utils"
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

	IgnoreMenuMessageID        int
	IgnoreContainerMap         map[int]string
	IgnoreContainerName        string
	IgnoreListPage             int
	PendingIgnoreContainerName string
}

func (b *Bot) handleCommand(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	state := b.getState(chatID)
	state.mu.Lock()
	defer state.mu.Unlock()

	wasPending := state.PendingIgnoreContainerName != ""
	if wasPending {
		b.cancelPendingIgnore(chatID, state)
	}

	switch msg.Command() {
	case "start":
		b.cmdStart(chatID, state)
	case "check":
		b.cmdCheck(chatID, state)
	case "ignore":
		b.cmdIgnore(chatID, state, msg.CommandArguments())
	case "ignore_list":
		b.showIgnoreList(chatID, state)
	case "cancel":
		b.cancelAllInteractions(chatID, state, wasPending)
	case "list":
		if state.LastMessageID != 0 {
			b.notifier.DeleteMessage(chatID, state.LastMessageID)
		}
		state.LastMessageID = 0
		state.View = viewMain
		b.showMainList(chatID, state)
	}
}

func (b *Bot) handleTextMessage(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}

	state := b.getState(chatID)
	state.mu.Lock()
	defer state.mu.Unlock()

	if state.PendingIgnoreContainerName == "" {
		return
	}

	b.notifier.DeleteMessage(chatID, msg.MessageID)
	b.handlePendingIgnoreText(chatID, state, text)
}

func (b *Bot) handleCallback(query *tgbotapi.CallbackQuery) {
	chatID := query.Message.Chat.ID
	data := query.Data
	state := b.getState(chatID)
	state.mu.Lock()
	defer state.mu.Unlock()

	switch {
	case data == "back":
		b.notifier.AnswerCallbackQuery(query.ID, "")
		b.handleBack(chatID, state)
	case data == "refresh":
		b.notifier.AnswerCallbackQuery(query.ID, "")
		b.refreshView(chatID, state)

	case strings.HasPrefix(data, "proj:"):
		b.notifier.AnswerCallbackQuery(query.ID, "")
		idx := 0
		fmt.Sscanf(strings.TrimPrefix(data, "proj:"), "%d", &idx)
		if name, ok := state.ProjectMap[idx]; ok {
			state.ProjectName = name
			state.View = viewProject
			b.showProjectViewMode(chatID, state, true)
		}

	case strings.HasPrefix(data, "cnt:"):
		b.notifier.AnswerCallbackQuery(query.ID, "")
		shortID := strings.TrimPrefix(data, "cnt:")
		state.View = viewContainer
		b.showContainerDetailMode(chatID, shortID, state, true)

	case strings.HasPrefix(data, "act:"):
		b.notifier.AnswerCallbackQuery(query.ID, "")
		b.handleContainerAction(chatID, data, state)

	case strings.HasPrefix(data, "pact:"):
		b.notifier.AnswerCallbackQuery(query.ID, "")
		b.handleProjectAction(chatID, data, state)

	case data == "ig:refresh" || data == "ig:back" || data == "ig:backtolist" ||
		data == "igadd" || data == "noop" ||
		strings.HasPrefix(data, "igcnt:") || strings.HasPrefix(data, "igpage:") ||
		strings.HasPrefix(data, "igrule:") || strings.HasPrefix(data, "igdel:"):
		b.notifier.AnswerCallbackQuery(query.ID, "")
		b.handleIgnoreCallback(chatID, data, state)

	default:
		b.notifier.AnswerCallbackQuery(query.ID, "")
	}
}

func (b *Bot) cmdStart(chatID int64, state *State) {
	text := "👋 <b>Docker Monitor</b>\n\n" +
		"<b>Commands:</b>\n" +
		"/check — quick status overview\n" +
		"/list — interactive container management\n" +
		"/ignore_list — manage log ignore rules\n" +
		"/ignore — manage ignore rules via text commands\n" +
		"/cancel — cancel current pending action"

	b.notifier.SendText(chatID, text)
}

func (b *Bot) cmdCheck(chatID int64, state *State) {
	ctx := context.Background()
	groups, err := b.Docker.GetContainerGroups(ctx)
	if err != nil {
		b.notifier.SendText(chatID, fmt.Sprintf("❌ Failed to fetch containers: %v", err))
		return
	}

	if len(groups.Projects) == 0 && len(groups.Standalone) == 0 {
		b.notifier.SendText(chatID, "🔍 <b>No containers found</b>")
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

	b.notifier.SendText(chatID, sb.String())
}

func (b *Bot) cmdIgnore(chatID int64, state *State, args string) {
	args = strings.TrimSpace(args)
	if args == "" || strings.EqualFold(args, "help") {
		b.notifier.SendText(chatID, ignoreHelpText())
		return
	}

	parts := strings.Fields(args)
	if len(parts) == 0 {
		b.notifier.SendText(chatID, ignoreHelpText())
		return
	}

	switch strings.ToLower(parts[0]) {
	case "add":
		rest := strings.TrimSpace(strings.TrimPrefix(args, parts[0]))
		scope, match, err := parseIgnoreAddArgs(rest)
		if err != nil {
			b.notifier.SendText(chatID, "❌ "+err.Error())
			return
		}

		rule, err := b.Docker.AddLogIgnoreRule(scope, match)
		if err != nil {
			b.notifier.SendText(chatID, fmt.Sprintf("❌ Failed to add ignore rule: %v", err))
			return
		}

		b.notifier.SendText(chatID, fmt.Sprintf(
			"✅ <b>Ignore rule added</b>\n\nID: <code>%d</code>\nScope: <code>%s</code>\nMatch: <code>%s</code>",
			rule.ID,
			utils.EscapeHTML(rule.ScopeSpec()),
			utils.EscapeHTML(rule.Match),
		))
	case "remove", "delete", "del":
		if len(parts) < 2 {
			b.notifier.SendText(chatID, "❌ Rule ID is required. Example: /ignore remove 2")
			return
		}

		id, err := strconv.Atoi(parts[1])
		if err != nil || id <= 0 {
			b.notifier.SendText(chatID, "❌ Rule ID must be a positive number")
			return
		}

		rules := b.Docker.ListLogIgnoreRules()
		var found *docker.LogIgnoreRule
		for i := range rules {
			if rules[i].ID == id {
				found = &rules[i]
				break
			}
		}

		deleted, err := b.Docker.DeleteLogIgnoreRule(id)
		if err != nil {
			b.notifier.SendText(chatID, fmt.Sprintf("❌ Failed to remove ignore rule: %v", err))
			return
		}
		if !deleted {
			b.notifier.SendText(chatID, fmt.Sprintf("❌ Ignore rule %d not found", id))
			return
		}

		if found != nil {
			b.notifier.SendText(chatID, fmt.Sprintf(
				"✅ <b>Ignore rule deleted</b>\n\nID: <code>%d</code>\nScope: <code>%s</code>\nMatch: <code>%s</code>",
				found.ID,
				utils.EscapeHTML(found.ScopeSpec()),
				utils.EscapeHTML(found.Match),
			))
		} else {
			b.notifier.SendText(chatID, fmt.Sprintf("✅ Ignore rule <code>%d</code> removed", id))
		}
	default:
		b.notifier.SendText(chatID, ignoreHelpText())
	}
}

func (b *Bot) showMainList(chatID int64, state *State) {
	b.showMainListMode(chatID, state, false)
}

func (b *Bot) showMainListMode(chatID int64, state *State, editExisting bool) {
	ctx := context.Background()
	groups, err := b.Docker.GetContainerGroups(ctx)
	if err != nil {
		b.sendOrEditErrorMode(chatID, state, "Failed to fetch containers", editExisting)
		return
	}

	if len(groups.Projects) == 0 && len(groups.Standalone) == 0 {
		b.sendOrEditMode(chatID, state, "🔍 <b>No containers found</b>", editExisting)
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

	b.replaceListMessage(chatID, state, msgText, keyboard, editExisting)
}

func (b *Bot) showProjectView(chatID int64, state *State) {
	b.showProjectViewMode(chatID, state, false)
}

func (b *Bot) showProjectViewMode(chatID int64, state *State, editExisting bool) {
	ctx := context.Background()
	groups, err := b.Docker.GetContainerGroups(ctx)
	if err != nil {
		b.sendOrEditErrorMode(chatID, state, "Failed to fetch containers", editExisting)
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
		b.sendOrEditErrorMode(chatID, state, "Project not found", editExisting)
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

	b.replaceListMessage(chatID, state, msgText, keyboard, editExisting)
}

func (b *Bot) showContainerDetail(chatID int64, shortID string, state *State) {
	b.showContainerDetailMode(chatID, shortID, state, false)
}

func (b *Bot) showContainerDetailMode(chatID int64, shortID string, state *State, editExisting bool) {
	fullID, ok := state.ShortIDMap[shortID]
	if !ok {
		b.sendOrEditErrorMode(chatID, state, "Container not found", editExisting)
		return
	}

	ctx := context.Background()
	info, err := b.Docker.ContainerInspect(ctx, fullID)
	if err != nil {
		b.sendOrEditErrorMode(chatID, state, "Container not found", editExisting)
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

	b.replaceListMessage(chatID, state, sb.String(), keyboard, editExisting)
}

func (b *Bot) handleContainerAction(chatID int64, data string, state *State) {
	parts := strings.SplitN(data, ":", 3)
	if len(parts) < 3 {
		return
	}
	action := parts[1]
	shortID := parts[2]

	fullID, ok := state.ShortIDMap[shortID]
	if !ok {
		b.sendOrEditErrorMode(chatID, state, "Container not found", editExistingList(state))
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
		b.sendOrEditErrorMode(chatID, state, fmt.Sprintf("Failed to %s: %v", action, err), editExistingList(state))
		return
	}

	time.Sleep(500 * time.Millisecond)

	b.showContainerDetailMode(chatID, shortID, state, true)
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
		b.sendOrEditErrorMode(chatID, state, "No project selected", editExistingList(state))
		return
	}

	ctx := context.Background()

	proj, err := b.findProject(ctx, projectName)
	if err != nil {
		b.sendOrEditErrorMode(chatID, state, fmt.Sprintf("Failed to find project: %v", err), editExistingList(state))
		return
	}

	verb := actionVerb(action)
	if state.LastMessageID != 0 {
		b.notifier.EditMessageText(chatID, state.LastMessageID,
			fmt.Sprintf("⏳ <b>%s %s...</b>", verb, projectName))
	}

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
		b.sendOrEditErrorMode(chatID, state, fmt.Sprintf("Failed to %s project: %v", action, err), editExistingList(state))
		return
	}

	time.Sleep(time.Second)

	b.showProjectViewMode(chatID, state, true)
}

func (b *Bot) handleBack(chatID int64, state *State) {
	switch state.View {
	case viewContainer:
		if state.ProjectName != "" {
			state.View = viewProject
			b.showProjectViewMode(chatID, state, true)
		} else {
			state.View = viewMain
			b.showMainListMode(chatID, state, true)
		}
	case viewProject:
		state.View = viewMain
		state.ProjectName = ""
		b.showMainListMode(chatID, state, true)
	default:
		state.View = viewMain
		b.showMainListMode(chatID, state, true)
	}
}

func (b *Bot) refreshView(chatID int64, state *State) {
	switch state.View {
	case viewProject:
		b.showProjectViewMode(chatID, state, true)
	case viewContainer:
		shortID := ""
		for currentShortID, fullID := range state.ShortIDMap {
			if fullID == state.ContainerID {
				shortID = currentShortID
				break
			}
		}
		if shortID != "" {
			b.showContainerDetailMode(chatID, shortID, state, true)
			return
		}
		b.showMainListMode(chatID, state, true)
	default:
		b.showMainListMode(chatID, state, true)
	}
}

func (b *Bot) replaceListMessage(chatID int64, state *State, text string, keyboard tgbotapi.InlineKeyboardMarkup, editExisting bool) {
	if editExisting && state.LastMessageID != 0 {
		b.notifier.EditMessageWithKeyboard(chatID, state.LastMessageID, text, keyboard)
		return
	}
	if state.LastMessageID != 0 {
		b.notifier.DeleteMessage(chatID, state.LastMessageID)
		state.LastMessageID = 0
	}
	state.LastMessageID = b.notifier.SendTextWithKeyboard(chatID, text, keyboard)
}

func (b *Bot) sendOrEdit(chatID int64, state *State, text string) {
	b.sendOrEditMode(chatID, state, text, false)
}

func (b *Bot) sendOrEditMode(chatID int64, state *State, text string, editExisting bool) {
	if editExisting && state.LastMessageID > 0 {
		b.notifier.EditMessageText(chatID, state.LastMessageID, text)
		return
	}
	if state.LastMessageID > 0 {
		b.notifier.DeleteMessage(chatID, state.LastMessageID)
		state.LastMessageID = 0
	}
	state.LastMessageID = b.notifier.SendText(chatID, text)
}

func (b *Bot) sendOrEditError(chatID int64, state *State, text string) {
	b.sendOrEditErrorMode(chatID, state, text, false)
}

func (b *Bot) sendOrEditErrorMode(chatID int64, state *State, text string, editExisting bool) {
	b.sendOrEditMode(chatID, state, "❌ "+text, editExisting)
}

func editExistingList(state *State) bool {
	return state.LastMessageID != 0
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

func (b *Bot) cancelAllInteractions(chatID int64, state *State, wasPendingIgnore bool) {
	cancelled := wasPendingIgnore

	if state.LastMessageID != 0 {
		b.notifier.DeleteMessage(chatID, state.LastMessageID)
		state.LastMessageID = 0
		state.View = viewMain
		state.ProjectName = ""
		state.ContainerID = ""
		cancelled = true
	}

	if state.IgnoreMenuMessageID != 0 {
		b.notifier.DeleteMessage(chatID, state.IgnoreMenuMessageID)
		state.IgnoreMenuMessageID = 0
		cancelled = true
	}

	if cancelled {
		b.notifier.SendText(chatID, "❌ Cancelled.")
	}
}

func parseIgnoreAddArgs(args string) (string, string, error) {
	parts := strings.SplitN(args, "|", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("Use format: /ignore add scope | text")
	}

	scope := strings.TrimSpace(parts[0])
	match := strings.TrimSpace(parts[1])
	if scope == "" {
		return "", "", fmt.Errorf("Ignore scope is required")
	}
	if match == "" {
		return "", "", fmt.Errorf("Ignore text is required")
	}

	if !strings.Contains(scope, ":") && strings.Contains(scope, "/") {
		scope = "service:" + scope
	} else if !strings.Contains(scope, ":") && !strings.EqualFold(scope, "global") && scope != "*" {
		scope = "container:" + scope
	}

	return scope, match, nil
}

func ignoreHelpText() string {
	return "🛠 <b>Log Ignore Rules</b>\n\n" +
		"<b>Commands:</b>\n" +
		"/ignore_list — interactive ignore rules menu\n" +
		"/ignore add any-sync-bundle | unable to connect\n" +
		"/ignore add any-sync-bundle/any-sync-bundle | space is missing\n" +
		"/ignore add global | unable to connect\n" +
		"/ignore add project:any-sync-bundle | space is missing\n" +
		"/ignore add service:any-sync-bundle/any-sync-bundle | unable to connect\n" +
		"/ignore add container:portainer | harmless warning\n" +
		"/ignore remove 2\n\n" +
		"<b>Default:</b> <code>name</code> means <code>container:name</code>, and <code>project/service</code> means <code>service:project/service</code>.\n" +
		"<b>Scope types:</b> global, project:&lt;name&gt;, service:&lt;project/service&gt;, container:&lt;name&gt;"
}

func (b *Bot) formatIgnoreRules() string {
	rules := b.Docker.ListLogIgnoreRules()
	if len(rules) == 0 {
		return "🧹 <b>No log ignore rules configured</b>\n\nUse <code>/ignore add scope | text</code> to add one."
	}

	var sb strings.Builder
	sb.WriteString("🧹 <b>Log Ignore Rules</b>\n")
	for _, rule := range rules {
		sb.WriteString(fmt.Sprintf(
			"\n<code>%d</code>. <code>%s</code>\nMatch: <code>%s</code>\n",
			rule.ID,
			utils.EscapeHTML(rule.ScopeSpec()),
			utils.EscapeHTML(rule.Match),
		))
	}

	return sb.String()
}
