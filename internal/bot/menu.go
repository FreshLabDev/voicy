// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"strconv"
	"strings"

	"github.com/FreshLabDev/voicy/internal/stats"
	"github.com/FreshLabDev/voicy/internal/telegram"
	"github.com/FreshLabDev/voicy/internal/transcript"
)

const menuPrefix = "m"

func cb(owner int64, action string) string {
	return menuPrefix + ":" + strconv.FormatInt(owner, 10) + ":" + action
}

func parseMenuCB(data string) (owner int64, action string, ok bool) {
	parts := strings.SplitN(data, ":", 3)
	if len(parts) != 3 || parts[0] != menuPrefix || parts[2] == "" {
		return 0, "", false
	}
	owner, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || owner == 0 {
		return 0, "", false
	}
	return owner, parts[2], true
}

func homePanel(lang string, owner int64) (string, *telegram.InlineKeyboardMarkup) {
	kb := &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{
			{Text: transcript.BtnStats(lang), CallbackData: cb(owner, "stats"), Style: telegram.StylePrimary},
			{Text: transcript.BtnHelp(lang), CallbackData: cb(owner, "help")},
		},
		{{Text: transcript.BtnClose(lang), CallbackData: cb(owner, "close"), Style: telegram.StyleDanger}},
	}}
	return transcript.HomeText(lang), kb
}

func helpPanel(lang string, owner int64) (string, *telegram.InlineKeyboardMarkup) {
	return transcript.HelpText(lang), navMarkup(lang, owner)
}

func statsPanel(lang string, owner int64, snap stats.Snapshot) (string, *telegram.InlineKeyboardMarkup) {
	return transcript.StatsText(lang, snap), navMarkup(lang, owner)
}

func navMarkup(lang string, owner int64) *telegram.InlineKeyboardMarkup {
	return &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{
			{Text: transcript.BtnBack(lang), CallbackData: cb(owner, "home")},
			{Text: transcript.BtnClose(lang), CallbackData: cb(owner, "close"), Style: telegram.StyleDanger},
		},
	}}
}
