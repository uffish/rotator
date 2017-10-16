package main

import (
	"fmt"

	"github.com/nlopes/slack"
)

func doSlackNotify(message string, destination string) error {
	var err error

	slackAPI := slack.New(config.SlackKey)
	params := slack.PostMessageParameters{}
	/* attach := slack.Attachment{
			Pretext: "my pretext",
			Text:    "some text",
		}
	  params.Attachments = []slack.Attachment{attach} */
	params.Username = "rotator"
	params.IconEmoji = ":umbrella:"
	channel := config.SlackChannel
	if destination != "" {
		channel = destination
	}
	if *flagDebug {
		fmt.Printf("Attempting to send %s to %s with token %s\n", message, channel, config.SlackKey)
	}
	c, timestamp, err := slackAPI.PostMessage(channel, message, params)
	if err != nil {
		fmt.Printf("%s\n", err)
		return err
	}
	if *flagDebug {
		fmt.Printf("Message sent successfully to channel %s at %s\n", c, timestamp)
	}
	return err
}
