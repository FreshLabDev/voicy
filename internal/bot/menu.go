// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"strconv"
	"strings"

	"github.com/FreshLabDev/tg"
	"github.com/FreshLabDev/voicy/internal/i18n"
	"github.com/FreshLabDev/voicy/internal/settings"
	"github.com/FreshLabDev/voicy/internal/stats"
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

func homePanel(lang string, owner int64) (string, *tg.InlineKeyboardMarkup) {
	kb := &tg.InlineKeyboardMarkup{InlineKeyboard: [][]tg.InlineKeyboardButton{
		{
			{Text: transcript.BtnLanguage(lang), CallbackData: cb(owner, "lang"), Style: tg.StylePrimary},
			{Text: transcript.BtnStats(lang), CallbackData: cb(owner, "stats")},
		},
		{
			{Text: transcript.BtnSettings(lang), CallbackData: cb(owner, "set")},
			{Text: transcript.BtnHelp(lang), CallbackData: cb(owner, "help")},
		},
		{
			{Text: transcript.BtnAbout(lang), CallbackData: cb(owner, "about")},
			{Text: transcript.BtnClose(lang), CallbackData: cb(owner, "close"), Style: tg.StyleDanger},
		},
	}}
	return transcript.HomeText(lang), kb
}

func helpPanel(lang string, owner int64) (string, *tg.InlineKeyboardMarkup) {
	return transcript.HelpText(lang), navMarkup(lang, owner)
}

func statsPanel(lang string, owner int64, snap stats.Snapshot, global bool) (string, *tg.InlineKeyboardMarkup) {
	personalLabel, globalLabel := transcript.TabPersonal(lang), transcript.TabGlobal(lang)
	if global {
		globalLabel = toggleOn + globalLabel
	} else {
		personalLabel = toggleOn + personalLabel
	}
	kb := &tg.InlineKeyboardMarkup{InlineKeyboard: [][]tg.InlineKeyboardButton{
		{
			{Text: personalLabel, CallbackData: cb(owner, "statsp")},
			{Text: globalLabel, CallbackData: cb(owner, "statsg")},
		},
	}}
	return transcript.StatsText(lang, snap, global), withNav(kb, lang, owner)
}

func aboutPanel(lang string, owner int64) (string, *tg.InlineKeyboardMarkup) {
	return transcript.AboutText(lang), navMarkup(lang, owner)
}

// languagePanel lists every language the fleet shares, two per row. The flag and
// native name come from i18n so Voicy's picker looks like searchy's and vido's.
func languagePanel(lang string, owner int64) (string, *tg.InlineKeyboardMarkup) {
	opts := i18n.LANGUAGE_OPTIONS
	rows := make([][]tg.InlineKeyboardButton, 0, (len(opts)+1)/2)
	for i := 0; i < len(opts); i += 2 {
		row := []tg.InlineKeyboardButton{languageButton(opts[i], lang, owner)}
		if i+1 < len(opts) {
			row = append(row, languageButton(opts[i+1], lang, owner))
		}
		rows = append(rows, row)
	}
	kb := &tg.InlineKeyboardMarkup{InlineKeyboard: rows}
	return transcript.LanguageText(lang), withNav(kb, lang, owner)
}

func languageButton(opt i18n.LangOption, lang string, owner int64) tg.InlineKeyboardButton {
	return tg.InlineKeyboardButton{
		Text:         toggleMark(opt.Code == lang) + opt.Label,
		CallbackData: cb(owner, "lang:"+opt.Code),
	}
}

func settingsPanel(lang string, owner int64, s settings.Settings) (string, *tg.InlineKeyboardMarkup) {
	var rows [][]tg.InlineKeyboardButton
	keys := settings.Keys()
	for i := 0; i < len(keys); i += 2 {
		var row []tg.InlineKeyboardButton
		for _, key := range keys[i:min(i+2, len(keys))] {
			next := "1"
			if s.IsOn(key) {
				next = "0"
			}
			row = append(row, tg.InlineKeyboardButton{
				Text:         toggleMark(s.IsOn(key)) + transcript.SettingLabel(lang, key),
				CallbackData: cb(owner, "set:"+key+":"+next),
			})
		}
		rows = append(rows, row)
	}
	kb := &tg.InlineKeyboardMarkup{InlineKeyboard: rows}
	return transcript.SettingsText(lang), withNav(kb, lang, owner)
}

func toggleMark(on bool) string {
	if on {
		return toggleOn
	}
	return toggleOff
}

func navMarkup(lang string, owner int64) *tg.InlineKeyboardMarkup {
	return &tg.InlineKeyboardMarkup{InlineKeyboard: [][]tg.InlineKeyboardButton{
		{
			{Text: transcript.BtnBack(lang), CallbackData: cb(owner, "home")},
			{Text: transcript.BtnClose(lang), CallbackData: cb(owner, "close"), Style: tg.StyleDanger},
		},
	}}
}

func withNav(kb *tg.InlineKeyboardMarkup, lang string, owner int64) *tg.InlineKeyboardMarkup {
	kb.InlineKeyboard = append(kb.InlineKeyboard,
		[]tg.InlineKeyboardButton{
			{Text: transcript.BtnBack(lang), CallbackData: cb(owner, "home")},
			{Text: transcript.BtnClose(lang), CallbackData: cb(owner, "close"), Style: tg.StyleDanger},
		})
	return kb
}
