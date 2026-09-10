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

// scope is which of the two screens a panel belongs to. Telegram gives Voicy the
// same panel two very different places: in a direct chat the whole conversation
// is the panel, while in a group it is one message sitting in everybody else's
// feed.
type scope int

const (
	scopePrivate scope = iota
	scopeGroup
)

func scopeOf(chat tg.Chat) scope {
	if chat.Type == "private" {
		return scopePrivate
	}
	return scopeGroup
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

// homePanel is two screens, not one. The direct chat gets every tab, because
// there the panel is the bot's whole interface. A group gets what a group can
// act on: settings and the interface language are personal and shared with the
// sibling bots, so offering them from somebody else's group would promise a
// local effect Voicy does not have.
func homePanel(lang string, owner int64, sc scope) (string, *tg.InlineKeyboardMarkup) {
	if sc == scopeGroup {
		return transcript.GroupHomeText(lang), &tg.InlineKeyboardMarkup{InlineKeyboard: [][]tg.InlineKeyboardButton{{
			{Text: transcript.BtnAbout(lang), CallbackData: cb(owner, "about")},
			{Text: transcript.BtnClose(lang), CallbackData: cb(owner, "close"), Style: tg.StyleDanger},
		}}}
	}
	kb := &tg.InlineKeyboardMarkup{InlineKeyboard: [][]tg.InlineKeyboardButton{
		{
			// Nothing else on this screen is readable until the language is
			// right, which is what makes it the one highlighted button.
			{Text: transcript.BtnLanguage(lang), CallbackData: cb(owner, "lang"), Style: tg.StylePrimary},
			{Text: transcript.BtnStats(lang), CallbackData: cb(owner, "stats")},
		},
		{
			{Text: transcript.BtnSettings(lang), CallbackData: cb(owner, "set")},
			{Text: transcript.BtnHelp(lang), CallbackData: cb(owner, "help")},
		},
		{
			{Text: transcript.BtnAbout(lang), CallbackData: cb(owner, "about")},
		},
	}}
	return transcript.HomeText(lang), kb
}

func helpPanel(lang string, owner int64, sc scope) (string, *tg.InlineKeyboardMarkup) {
	return transcript.HelpText(lang), navMarkup(lang, owner, sc)
}

func statsPanel(lang string, owner int64, snap stats.Snapshot, global bool, sc scope) (string, *tg.InlineKeyboardMarkup) {
	// The two tabs are one set, so both carry a glyph and the pair has one left
	// edge; the open one is the state the reader is in, not an errand they came
	// to run, so it is Success and this screen leads nowhere in Primary.
	personal := tg.InlineKeyboardButton{Text: toggleMark(!global) + transcript.TabPersonal(lang), CallbackData: cb(owner, "statsp")}
	worldwide := tg.InlineKeyboardButton{Text: toggleMark(global) + transcript.TabGlobal(lang), CallbackData: cb(owner, "statsg")}
	if global {
		worldwide.Style = tg.StyleSuccess
	} else {
		personal.Style = tg.StyleSuccess
	}
	kb := &tg.InlineKeyboardMarkup{InlineKeyboard: [][]tg.InlineKeyboardButton{{personal, worldwide}}}
	return transcript.StatsText(lang, snap, global), withNav(kb, lang, owner, sc)
}

func aboutPanel(lang string, owner int64, version string, sc scope) (string, *tg.InlineKeyboardMarkup) {
	return transcript.AboutText(lang, version), navMarkup(lang, owner, sc)
}

// languagePanel lists every language the fleet shares, two per row. The flag and
// native name come from i18n so Voicy's picker looks like searchy's and vido's.
//
// Under the grid sits the way back out. Picking a language by hand writes a
// manual observation into the shared core hub, and manual outranks every
// automatic source for ever: without this button somebody who tapped the wrong
// flag once would read Voicy in that language until a human touched the
// database. It is not styled, because following the client is not more likely
// to be what the reader came for than picking a language is.
func languagePanel(lang string, owner int64, sc scope) (string, *tg.InlineKeyboardMarkup) {
	opts := i18n.LANGUAGE_OPTIONS
	rows := make([][]tg.InlineKeyboardButton, 0, (len(opts)+1)/2+1)
	for i := 0; i < len(opts); i += 2 {
		row := []tg.InlineKeyboardButton{languageButton(opts[i], lang, owner)}
		if i+1 < len(opts) {
			row = append(row, languageButton(opts[i+1], lang, owner))
		}
		rows = append(rows, row)
	}
	rows = append(rows, []tg.InlineKeyboardButton{
		{Text: transcript.BtnFollowTelegram(lang), CallbackData: cb(owner, "langauto")},
	})
	kb := &tg.InlineKeyboardMarkup{InlineKeyboard: rows}
	return transcript.LanguageText(lang), withNav(kb, lang, owner, sc)
}

// languageButton styles the current language and nothing else: one green button
// in a grid of sixteen answers "which one am I on?" before the glyph is read.
func languageButton(opt i18n.LangOption, lang string, owner int64) tg.InlineKeyboardButton {
	btn := tg.InlineKeyboardButton{
		Text:         toggleMark(opt.Code == lang) + opt.Label,
		CallbackData: cb(owner, "lang:"+opt.Code),
	}
	if opt.Code == lang {
		btn.Style = tg.StyleSuccess
	}
	return btn
}

// settingsPanel shows every switch on its glyph and none of them in colour.
//
// The family rule reserves Success for state and forbids it on a button that
// acts, and a toggle is both at once: it reports that the setting is on, and
// tapping it turns the setting off. Painting it green puts the colour for
// "this is how things are" on the control that undoes it — the current-language
// button is inert by comparison, which is why that one is coloured and these
// are not. The glyph carries the state here, and seven greens would not have
// distinguished anything anyway: a mark is a signal when it is on one option
// out of a set, and wallpaper when it can be on all of them at once.
func settingsPanel(lang string, owner int64, s settings.Settings, sc scope) (string, *tg.InlineKeyboardMarkup) {
	var rows [][]tg.InlineKeyboardButton
	keys := settings.Keys()
	for i := 0; i < len(keys); i += 2 {
		var row []tg.InlineKeyboardButton
		for _, key := range keys[i:min(i+2, len(keys))] {
			next := "1"
			if s.IsOn(key) {
				next = "0"
			}
			btn := tg.InlineKeyboardButton{
				Text:         toggleMark(s.IsOn(key)) + transcript.SettingLabel(lang, key),
				CallbackData: cb(owner, "set:"+key+":"+next),
			}
			row = append(row, btn)
		}
		rows = append(rows, row)
	}
	kb := &tg.InlineKeyboardMarkup{InlineKeyboard: rows}
	return transcript.SettingsText(lang), withNav(kb, lang, owner, sc)
}

func toggleMark(on bool) string {
	if on {
		return toggleOn
	}
	return toggleOff
}

// navRow offers Close only where there is something to close. A group panel is
// Voicy's message in a conversation that belongs to other people and clearing it
// away is the polite exit, so it is offered and marked destructive. In a direct
// chat the conversation is the panel: closing it deletes the message the person
// is looking at and leaves them staring at their own history.
func navRow(lang string, owner int64, sc scope) []tg.InlineKeyboardButton {
	row := []tg.InlineKeyboardButton{{Text: transcript.BtnBack(lang), CallbackData: cb(owner, "home")}}
	if sc == scopeGroup {
		row = append(row, tg.InlineKeyboardButton{Text: transcript.BtnClose(lang), CallbackData: cb(owner, "close"), Style: tg.StyleDanger})
	}
	return row
}

func navMarkup(lang string, owner int64, sc scope) *tg.InlineKeyboardMarkup {
	return &tg.InlineKeyboardMarkup{InlineKeyboard: [][]tg.InlineKeyboardButton{navRow(lang, owner, sc)}}
}

func withNav(kb *tg.InlineKeyboardMarkup, lang string, owner int64, sc scope) *tg.InlineKeyboardMarkup {
	kb.InlineKeyboard = append(kb.InlineKeyboard, navRow(lang, owner, sc))
	return kb
}
