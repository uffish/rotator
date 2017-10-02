package main

import (
	"fmt"

	"github.com/nlopes/slack"
)

func notifySlack(message string, destination string) {

	slackAPI := slack.New(config.SlackKey)
	params := slack.PostMessageParameters{}
	/* attach := slack.Attachment{
			Pretext: "my pretext",
			Text:    "some text",
		}
	  params.Attachments = []slack.Attachment{attach} */
	channel := config.SlackChannel
	if destination != "" {
		channel = destination
	}

	c, timestamp, err := slackAPI.PostMessage(channel, message, params)
	if err != nil {
		fmt.Printf("%s\n", err)
		return
	}
	fmt.Printf("Message sent to channel %s at %s", c, timestamp)
}
