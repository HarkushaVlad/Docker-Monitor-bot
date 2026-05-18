package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/HarkushaVlad/docker-monitor-bot/internal/docker"
	"github.com/HarkushaVlad/docker-monitor-bot/internal/utils"
)

const ignorePageSize = 5

type containerIgnoreEntry struct {
	displayName   string
	containerName string
	ignoreCount   int
}

func (b *Bot) listContainersForIgnore(ctx context.Context) ([]containerIgnoreEntry, error) {
	containers, err := b.Docker.ListUserContainers(ctx)
	if err != nil {
		return nil, err
	}

	rules := b.Docker.ListLogIgnoreRules()

	countByName := make(map[string]int)
	for _, r := range rules {
		if r.ScopeType == "container" {
			countByName[strings.ToLower(r.ScopeValue)]++
		}
	}

	entries := make([]containerIgnoreEntry, 0, len(containers))
	for _, c := range containers {
		name := utils.ContainerName(c)
		entries = append(entries, containerIgnoreEntry{
			displayName:   docker.ServiceName(c),
			containerName: name,
			ignoreCount:   countByName[strings.ToLower(name)],
		})
	}
	return entries, nil
}

func (b *Bot) rulesForContainer(containerName string) []docker.LogIgnoreRule {
	all := b.Docker.ListLogIgnoreRules()
	var result []docker.LogIgnoreRule
	for _, r := range all {
		if r.ScopeType == "container" && strings.EqualFold(r.ScopeValue, containerName) {
			result = append(result, r)
		}
	}
	return result
}

func (b *Bot) showIgnoreList(chatID int64, state *State) {
	ctx := context.Background()
	entries, err := b.listContainersForIgnore(ctx)
	if err != nil {
		b.notifier.SendText(chatID, "❌ Failed to fetch containers")
		return
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	state.IgnoreContainerMap = make(map[int]string)
	if len(entries) == 0 {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("(no containers)", "noop"),
		))
	}
	for i, e := range entries {
		label := fmt.Sprintf("📦 %s (%d)", e.displayName, e.ignoreCount)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("igcnt:%d", i)),
		))
		state.IgnoreContainerMap[i] = e.containerName
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🔄 Refresh", "ig:refresh"),
	))

	keyboard := tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	text := "🧹 <b>Log Ignore Rules</b>\n\nSelect a container to manage its ignore rules:"

	b.replaceIgnoreMenuMessage(chatID, state, text, keyboard)
}

func (b *Bot) showContainerIgnoreList(chatID int64, state *State, containerName string, page int) {
	rules := b.rulesForContainer(containerName)

	state.IgnoreContainerName = containerName

	totalPages := 1
	if len(rules) > 0 {
		totalPages = (len(rules) + ignorePageSize - 1) / ignorePageSize
	}
	if page < 0 {
		page = 0
	}
	if page >= totalPages {
		page = totalPages - 1
	}
	state.IgnoreListPage = page

	start := page * ignorePageSize
	end := start + ignorePageSize
	if end > len(rules) {
		end = len(rules)
	}
	pageRules := rules[start:end]

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🧹 <b>%s</b> — ignore rules", utils.EscapeHTML(containerName)))
	if len(rules) == 0 {
		sb.WriteString("\n\n<i>No rules yet. Use Add Ignore to create one.</i>")
	} else {
		sb.WriteString(fmt.Sprintf(" (%d total)", len(rules)))
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, r := range pageRules {
		label := fmt.Sprintf("#%d: %s", r.ID, truncate(r.Match, 30))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("igrule:%d", r.ID)),
		))
	}

	if totalPages > 1 {
		var navRow []tgbotapi.InlineKeyboardButton
		if page > 0 {
			navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("◀️ Prev", fmt.Sprintf("igpage:%d", page-1)))
		}
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("%d/%d", page+1, totalPages), "noop"))
		if page < totalPages-1 {
			navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("Next ▶️", fmt.Sprintf("igpage:%d", page+1)))
		}
		rows = append(rows, navRow)
	}

	rows = append(rows,
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ Add Ignore", "igadd"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("↩️ Back", "ig:back"),
		),
	)

	keyboard := tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	b.replaceIgnoreMenuMessage(chatID, state, sb.String(), keyboard)
}

func (b *Bot) showIgnoreRuleDetail(chatID int64, state *State, ruleID int) {
	rules := b.Docker.ListLogIgnoreRules()
	var found *docker.LogIgnoreRule
	for i := range rules {
		if rules[i].ID == ruleID {
			found = &rules[i]
			break
		}
	}
	if found == nil {
		b.notifier.SendText(chatID, "❌ Rule not found")
		return
	}

	text := fmt.Sprintf(
		"🧹 <b>Ignore Rule #%d</b>\n\nScope: <code>%s</code>\nMatch: <code>%s</code>",
		found.ID,
		utils.EscapeHTML(found.ScopeSpec()),
		utils.EscapeHTML(found.Match),
	)

	keyboard := tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🗑 Delete", fmt.Sprintf("igdel:%d", found.ID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("↩️ Back", "ig:backtolist"),
		),
	}}

	b.replaceIgnoreMenuMessage(chatID, state, text, keyboard)
}

func (b *Bot) enterPendingIgnore(chatID int64, state *State) {
	containerName := state.IgnoreContainerName
	text := fmt.Sprintf(
		"⌨️ <b>Add Ignore Rule</b>\n\nContainer: <code>%s</code>\n\nSend the text to match (substring, case-insensitive).\nSend /cancel to cancel.",
		utils.EscapeHTML(containerName),
	)
	b.deleteIgnoreMenuMessage(chatID, state)
	state.IgnoreMenuMessageID = b.notifier.SendText(chatID, text)
	state.PendingIgnoreContainerName = containerName
}

func (b *Bot) cancelPendingIgnore(chatID int64, state *State) {
	if state.PendingIgnoreContainerName == "" {
		return
	}
	b.deleteIgnoreMenuMessage(chatID, state)
	state.PendingIgnoreContainerName = ""
}

func (b *Bot) handlePendingIgnoreText(chatID int64, state *State, text string) bool {
	if state.PendingIgnoreContainerName == "" {
		return false
	}

	containerName := state.PendingIgnoreContainerName
	state.PendingIgnoreContainerName = ""
	b.deleteIgnoreMenuMessage(chatID, state)

	scopeSpec := "container:" + containerName
	rule, err := b.Docker.AddLogIgnoreRule(scopeSpec, text)
	if err != nil {
		b.notifier.SendText(chatID, fmt.Sprintf("❌ Failed to add ignore rule: %v", err))
	} else {
		b.notifier.SendText(chatID, fmt.Sprintf(
			"✅ <b>Ignore rule added</b>\n\nContainer: <code>%s</code>\nMatch: <code>%s</code>\nID: <code>%d</code>",
			utils.EscapeHTML(rule.ScopeValue),
			utils.EscapeHTML(rule.Match),
			rule.ID,
		))
	}

	b.showContainerIgnoreList(chatID, state, containerName, 0)
	return true
}

func (b *Bot) replaceIgnoreMenuMessage(chatID int64, state *State, text string, keyboard tgbotapi.InlineKeyboardMarkup) {
	b.deleteIgnoreMenuMessage(chatID, state)
	state.IgnoreMenuMessageID = b.notifier.SendTextWithKeyboard(chatID, text, keyboard)
}

func (b *Bot) deleteIgnoreMenuMessage(chatID int64, state *State) {
	if state.IgnoreMenuMessageID != 0 {
		b.notifier.DeleteMessage(chatID, state.IgnoreMenuMessageID)
		state.IgnoreMenuMessageID = 0
	}
}

func (b *Bot) handleIgnoreCallback(chatID int64, data string, state *State) {
	switch {
	case data == "ig:refresh":
		b.showIgnoreList(chatID, state)

	case data == "ig:back":
		b.showIgnoreList(chatID, state)

	case data == "ig:backtolist":
		b.showContainerIgnoreList(chatID, state, state.IgnoreContainerName, state.IgnoreListPage)

	case data == "igadd":
		b.enterPendingIgnore(chatID, state)

	case data == "noop":

	case strings.HasPrefix(data, "igcnt:"):
		idx, err := strconv.Atoi(strings.TrimPrefix(data, "igcnt:"))
		if err != nil || idx < 0 {
			b.notifier.SendText(chatID, "❌ Invalid container index")
			return
		}
		containerName, ok := state.IgnoreContainerMap[idx]
		if !ok {
			b.notifier.SendText(chatID, "❌ Container not found")
			return
		}
		b.showContainerIgnoreList(chatID, state, containerName, 0)

	case strings.HasPrefix(data, "igpage:"):
		page, err := strconv.Atoi(strings.TrimPrefix(data, "igpage:"))
		if err != nil {
			return
		}
		b.showContainerIgnoreList(chatID, state, state.IgnoreContainerName, page)

	case strings.HasPrefix(data, "igrule:"):
		ruleID, err := strconv.Atoi(strings.TrimPrefix(data, "igrule:"))
		if err != nil || ruleID <= 0 {
			return
		}
		b.showIgnoreRuleDetail(chatID, state, ruleID)

	case strings.HasPrefix(data, "igdel:"):
		ruleID, err := strconv.Atoi(strings.TrimPrefix(data, "igdel:"))
		if err != nil || ruleID <= 0 {
			return
		}
		b.handleIgnoreDelete(chatID, state, ruleID)
	}
}

func (b *Bot) handleIgnoreDelete(chatID int64, state *State, ruleID int) {
	rules := b.Docker.ListLogIgnoreRules()
	var found *docker.LogIgnoreRule
	for i := range rules {
		if rules[i].ID == ruleID {
			found = &rules[i]
			break
		}
	}

	deleted, err := b.Docker.DeleteLogIgnoreRule(ruleID)
	if err != nil {
		b.notifier.SendText(chatID, fmt.Sprintf("❌ Failed to delete rule: %v", err))
		return
	}
	if !deleted {
		b.notifier.SendText(chatID, fmt.Sprintf("❌ Rule #%d not found", ruleID))
		return
	}

	if found != nil {
		b.notifier.SendText(chatID, fmt.Sprintf(
			"✅ <b>Ignore rule deleted</b>\n\nID: <code>%d</code>\nScope: <code>%s</code>\nMatch: <code>%s</code>",
			found.ID,
			utils.EscapeHTML(found.ScopeSpec()),
			utils.EscapeHTML(found.Match),
		))
	}

	b.showContainerIgnoreList(chatID, state, state.IgnoreContainerName, state.IgnoreListPage)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
