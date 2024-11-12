package telegram

import (
	tele "gopkg.in/telebot.v3"
)

func (reporter *Reporter) HandleAddMute(c tele.Context) error {
	reporter.Logger.Info().
		Str("sender", c.Sender().Username).
		Str("text", c.Text()).
		Msg("Got add mute query")

	mute, err := ParseMuteOptions(c.Text(), c)
	if err != "" {
		return c.Reply("Error muting notification: " + err)
	}

	if insertErr := reporter.MutesManager.AddMute(mute); insertErr != nil {
		reporter.Logger.Error().Err(insertErr).Msg("Error adding mute")
		return reporter.BotReply(c, "Error adding mute")
	}

	templateRendered, renderErr := reporter.TemplatesManager.Render("mute_added", mute)
	if renderErr != nil {
		reporter.Logger.Error().Err(renderErr).Msg("Error rendering template")
		return reporter.BotReply(c, "Error rendering template")
	}

	return reporter.BotReply(c, templateRendered)
}
