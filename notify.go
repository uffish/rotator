package main

import (
	"fmt"
	"net/smtp"
	"os"
	"os/user"
	"strings"
	"time"
)

type Mail struct {
	Destination string
	Body        []string
	Subject     string
	Sender      string
}

// Send the oncall notification mail.
func doNotify(victim string, when string) error {
	address := ""
	slackID := ""
	var err error
	var mail Mail
	emergency := false

	d := time.Now()
	if when == "tomorrow" {
		d = d.AddDate(0, 0, 1)
	}

	if when == "emergency" {
		emergency = true
		when = "today"
	}

	dstring := d.Format("Mon 2 Jan")

	mail.Sender = config.MailSender
	mail.Subject = fmt.Sprintf("Reminder: You are on duty %s [%s]", when, dstring)
	if emergency == false {
		mail.Subject = fmt.Sprintf("Reminder: You are on duty %s [%s]", when, dstring)
		mail.Body = []string{fmt.Sprintf("Dear %s,", victim),
			fmt.Sprintf("This is to remind you that you are on duty %s.", when),
			"Have fun!",
			"",
			"May the queries flow and the pagers be silent.",
			" - the VSI onduty rotator",
		}
	} else {
		mail.Subject = fmt.Sprintf("Attention: You are on duty today! [%s]", dstring)
		mail.Body = []string{fmt.Sprintf("Dear %s,", victim),
			"You are on duty today as the person previously on call is",
			"unavailable on short notice. The on duty rota has therefore been moved",
			"up by one day.",
			"Have fun!",
			"",
			"May the queries flow and pagers be silent.",
			" - the VSI onduty rotator",
		}
	}
	for _, c := range config.Oncallers {
		if c.Code == victim {
			address = c.Email
			slackID = c.SlackID
		}
	}
	if address != "" {
		mail.Destination = address
	} else {
		// nobody to send it to
		return err
	}
	if config.MailServer == "" {
		config.MailServer = "localhost:25"
	}
	if config.SlackKey != "" {
		slackMessage := fmt.Sprintf("Hello %s! This is to remind you that you're on duty today.", victim)
		notifySlack(slackID, slackMessage)
	}
	err = mailSend(mail, config.MailServer)
	if err != nil {
		return err
	}
	return err
}

func mailSend(mail Mail, server string) error {

	if mail.Sender == "" {
		u, _ := user.Current()
		hn, _ := os.Hostname()
		mail.Sender = fmt.Sprintf("%s@%s", u.Username, hn)
	}

	headers := []string{fmt.Sprintf("To: %s", mail.Destination),
		fmt.Sprintf("From: %s", mail.Sender),
		fmt.Sprintf("Subject: %s", mail.Subject),
		"X-Mailer: VSI Reminder Mailer",
	}

	fulltext := strings.Join(headers, "\r\n") + "\r\n\r\n"
	// And add the body
	fulltext += strings.Join(mail.Body, "\r\n") + "\r\n"
	if *flagDebug {
		fmt.Printf("Sending mail:\n%s", fulltext)
	}
	err := smtp.SendMail(server,
		nil,
		mail.Sender,
		[]string{mail.Destination},
		[]byte(fulltext))
	return err
}
