package main

import (
	"fmt"

	"github.com/nlopes/slack"
)

func doSlackDM(message string, destination string) error {
	var destID string
	slackAPI := slack.New(config.SlackKey)
	userList, err := slackAPI.GetUsers()
	if err != nil {
		return err
	}
	for _, u := range userList {
		if u.Name == destination {
			destID = u.ID
		}
	}
	if destID == "" {
		if *flagDebug {
			fmt.Printf("user %s doesn't appear in Slack, fail silently\n", destination)
		}
		return nil
	}

	_, _, channel, err := slackAPI.OpenIMChannel(destID)
	if err != nil {
		return err
	}
	if *flagDebug {
		fmt.Printf("Opened DM channel ID %s to %s\n", channel, destID)
	}
	params := slack.PostMessageParameters{}
	params.Username = "rotator"
	params.IconEmoji = ":umbrella:"
	slackAPI.PostMessage(channel, message, params)
	slackAPI.CloseIMChannel(channel)
	return nil
}

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
