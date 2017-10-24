package main

import (
	"errors"
	"fmt"

	"github.com/nlopes/slack"
)

func getSlackParams() slack.PostMessageParameters {
	params := slack.NewPostMessageParameters()
	params.Username = "rotator"
	params.IconEmoji = ":umbrella:"
	return params
}

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
		return errors.New("Couldn't open IM channel")
	}

	slackAPI.PostMessage(channel, message, getSlackParams())
	if *flagDebug {
		fmt.Printf("Sent DM on channel ID %s to %s\n", channel, destID)
	}
	slackAPI.CloseIMChannel(channel)
	return err
}

func doSlackNotify(message string, destination string) error {

	slackAPI := slack.New(config.SlackKey)
	channel := config.SlackChannel
	if destination != "" {
		channel = destination
	}
	if *flagDebug {
		fmt.Printf("Attempting to send %s to %s with token %s\n", message, channel, config.SlackKey)
	}
	c, timestamp, err := slackAPI.PostMessage(channel, message, getSlackParams())
	if err != nil {
		return err
	}
	if *flagDebug {
		fmt.Printf("Message sent successfully to channel %s at %s\n", c, timestamp)
	}
	return err
}
