// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"strconv"
	"strings"

	"github.com/FreshLabDev/voicy/internal/settings"
	"github.com/FreshLabDev/voicy/internal/stats"
	"github.com/FreshLabDev/voicy/internal/telegram"
	"github.com/FreshLabDev/voicy/internal/transcript"
)

const menuPrefix = "m"

const (
	toggleOn  = "◉ "
	toggleOff = "◎ "
)

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
			{Text: transcript.BtnLanguage(lang), CallbackData: cb(owner, "lang"), Style: telegram.StylePrimary},
			{Text: transcript.BtnStats(lang), CallbackData: cb(owner, "stats")},
		},
		{
			{Text: transcript.BtnSettings(lang), CallbackData: cb(owner, "set")},
			{Text: transcript.BtnHelp(lang), CallbackData: cb(owner, "help")},
		},
		{
			{Text: transcript.BtnAbout(lang), CallbackData: cb(owner, "about")},
			{Text: transcript.BtnClose(lang), CallbackData: cb(owner, "close"), Style: telegram.StyleDanger},
		},
	}}
	return transcript.HomeText(lang), kb
}

func helpPanel(lang string, owner int64) (string, *telegram.InlineKeyboardMarkup) {
	return transcript.HelpText(lang), navMarkup(lang, owner)
}

func statsPanel(lang string, owner int64, snap stats.Snapshot, global bool) (string, *telegram.InlineKeyboardMarkup) {
	personalLabel, globalLabel := "My stats", "All Voicy"
	if lang == "ru" {
		personalLabel, globalLabel = "Моя", "Весь Voicy"
	}
	if global {
		globalLabel = toggleOn + globalLabel
	} else {
		personalLabel = toggleOn + personalLabel
	}
	kb := &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{
			{Text: personalLabel, CallbackData: cb(owner, "statsp")},
			{Text: globalLabel, CallbackData: cb(owner, "statsg")},
		},
	}}
	return transcript.StatsText(lang, snap, global), withNav(kb, lang, owner)
}

func aboutPanel(lang string, owner int64) (string, *telegram.InlineKeyboardMarkup) {
	return transcript.AboutText(lang), navMarkup(lang, owner)
}

func languagePanel(lang string, owner int64) (string, *telegram.InlineKeyboardMarkup) {
	ru, en := toggleOff, toggleOff
	if lang == "ru" {
		ru = toggleOn
	} else {
		en = toggleOn
	}
	kb := &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{
			{Text: ru + "Русский", CallbackData: cb(owner, "lang:ru")},
			{Text: en + "English", CallbackData: cb(owner, "lang:en")},
		},
	}}
	return transcript.LanguageText(lang), withNav(kb, lang, owner)
}

func settingsPanel(lang string, owner int64, s settings.Settings) (string, *telegram.InlineKeyboardMarkup) {
	var rows [][]telegram.InlineKeyboardButton
	keys := settings.Keys()
	for i := 0; i < len(keys); i += 2 {
		var row []telegram.InlineKeyboardButton
		for _, key := range keys[i:min(i+2, len(keys))] {
			next := "1"
			if s.IsOn(key) {
				next = "0"
			}
			row = append(row, telegram.InlineKeyboardButton{
				Text:         toggleMark(s.IsOn(key)) + transcript.SettingLabel(lang, key),
				CallbackData: cb(owner, "set:"+key+":"+next),
			})
		}
		rows = append(rows, row)
	}
	kb := &telegram.InlineKeyboardMarkup{InlineKeyboard: rows}
	return transcript.SettingsText(lang), withNav(kb, lang, owner)
}

func toggleMark(on bool) string {
	if on {
		return toggleOn
	}
	return toggleOff
}

func navMarkup(lang string, owner int64) *telegram.InlineKeyboardMarkup {
	return &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{
			{Text: transcript.BtnBack(lang), CallbackData: cb(owner, "home")},
			{Text: transcript.BtnClose(lang), CallbackData: cb(owner, "close"), Style: telegram.StyleDanger},
		},
	}}
}

func withNav(kb *telegram.InlineKeyboardMarkup, lang string, owner int64) *telegram.InlineKeyboardMarkup {
	kb.InlineKeyboard = append(kb.InlineKeyboard,
		[]telegram.InlineKeyboardButton{
			{Text: transcript.BtnBack(lang), CallbackData: cb(owner, "home")},
			{Text: transcript.BtnClose(lang), CallbackData: cb(owner, "close"), Style: telegram.StyleDanger},
		})
	return kb
}
