package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

var ogDateAliasString = string("20060102")
var ogTimeString = string("2006-01-02T15:04:05-07:00")

var ogOverrideCommand = string("overrides")
var ogScheduleCommand = string("timeline")
var ogURLPrefix = string("https://api.opsgenie.com/v2/schedules")

type ogConfig struct {
	APIKey          string
	ScheduleID      string
	WeekdaySchedule string
	WeekendSchedule string
}

type ogUser struct {
	Type     string `json:"type"`
	ID       string `json:"id"`
	Username string `json:"username"`
}

type ogOverride struct {
	Alias     string       `json:"alias"`
	User      ogUser       `json:"user"`
	StartDate string       `json:"startDate"`
	EndDate   string       `json:"endDate"`
	Rotations []ogRotation `json:"rotations"`
}

type ogOverrideResponse struct {
	Data      []ogOverride
	Took      int
	RequestID string
}

type ogOncallData struct {
	OncallRecipients []string
}

type ogOncallResponse struct {
	Data      ogOncallData
	Took      int
	RequestID string
}

// We don't need to care about the Order or Periods fields.
type ogRotation struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func setOpsgenieByDay(day time.Time, email string) bool {
	schedule := config.OpsGenie.WeekdaySchedule
	starttime := time.Date(day.Year(), day.Month(), day.Day(), 8, 0, 0, 0, time.Local)

	// figure out whether to use weekend or weekday schedule
	if day.Weekday() == 6 || day.Weekday() == 0 {
		schedule = config.OpsGenie.WeekendSchedule
		starttime = time.Date(day.Year(), day.Month(), day.Day(), 10, 0, 0, 0, time.Local)
	}

	if ogCheckForOverride(day) == true {
		ogUpdateOverride(starttime, schedule, email)
	} else {
		ogCreateOverride(starttime, schedule, email)
	}
	return false
}

func ogCheckForOverride(day time.Time) bool {
	u, _ := url.Parse(ogURLPrefix + "/" +
		config.OpsGenie.ScheduleID + "/" + ogOverrideCommand + "/" +
		day.Format(ogDateAliasString))
	cli := &http.Client{}
	req, _ := http.NewRequest("GET", u.String(), nil)
	req.Header.Add("Authorization",
		fmt.Sprintf("GenieKey %s", config.OpsGenie.APIKey))
	resp, err := cli.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return true
	}
	return false
}

func ogCreateOverride(starttime time.Time, schedule string, email string) bool {

	enddate := starttime.AddDate(0, 0, 1)
	endtime := time.Date(enddate.Year(), enddate.Month(), enddate.Day(), 0, 0, 0, 0, time.Local)

	var override ogOverride
	override.Alias = starttime.Format(ogDateAliasString)
	override.User.Type = "user"
	override.User.Username = email
	var rotation ogRotation
	rotation.Name = schedule
	override.Rotations = append(override.Rotations, rotation)
	// var ogTimeString = string("2006-01-02T15:04:05Z")
	// FIXME(mpk) Requires time zone +01:00
	override.StartDate = starttime.Format(ogTimeString)
	override.EndDate = endtime.Format(ogTimeString)
	body, _ := json.Marshal(override)

	u, _ := url.Parse(ogURLPrefix + "/" +
		config.OpsGenie.ScheduleID + "/" + ogOverrideCommand)
	cli := &http.Client{}
	req, _ := http.NewRequest("POST", u.String(), bytes.NewBuffer(body))
	req.Header.Add("Authorization",
		fmt.Sprintf("GenieKey %s", config.OpsGenie.APIKey))
	req.Header.Add("Content-Type", "application/json")
	resp, err := cli.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		if *flagDebug == true {
			fmt.Printf("Created %s for %s OK", override.Alias, rotation.Name)
		}
		return true
	}
	return false
}

func ogUpdateOverride(starttime time.Time, schedule string, email string) bool {

	enddate := starttime.AddDate(0, 0, 1)
	endtime := time.Date(enddate.Year(), enddate.Month(), enddate.Day(), 0, 0, 0, 0, time.Local)

	var override ogOverride
	override.Alias = starttime.Format(ogDateAliasString)
	override.User.Type = "user"
	override.User.Username = email
	var rotation ogRotation
	rotation.Name = schedule
	override.Rotations = append(override.Rotations, rotation)
	// var ogTimeString = string("2006-01-02T15:04:05Z")
	// FIXME(mpk) Requires time zone +01:00
	override.StartDate = starttime.Format(ogTimeString)
	override.EndDate = endtime.Format(ogTimeString)
	body, _ := json.Marshal(override)

	u, _ := url.Parse(ogURLPrefix + "/" +
		config.OpsGenie.ScheduleID + "/" + ogOverrideCommand + "/" +
		starttime.Format(ogDateAliasString))
	cli := &http.Client{}
	req, _ := http.NewRequest("PUT", u.String(), bytes.NewBuffer(body))
	req.Header.Add("Authorization",
		fmt.Sprintf("GenieKey %s", config.OpsGenie.APIKey))
	req.Header.Add("Content-Type", "application/json")
	resp, err := cli.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		if *flagDebug == true {
			fmt.Printf("Updated %s for %s OK", override.Alias, rotation.Name)
		}
		return true
	}
	return false
}

// func getOGURL(command string, args map[string]string) ([]byte, error) {
// 	u, _ := url.Parse(ogURLPrefix + "/" + scheduleID + "/" + command)
// 	values := url.Values{}
// 	for a, b := range args {
// 		values.Add(a, b)
// 	}
// 	u.RawQuery = values.Encode()
// 	cli := &http.Client{}
// 	fmt.Println(u.String())
// 	req, _ := http.NewRequest("GET", u.String(), nil)
// 	req.Header.Add("Authorization", authKey)
// 	resp, err := cli.Do(req)
// 	if err != nil {
// 		panic(err)
// 	}
// 	defer resp.Body.Close()
// 	body, _ := ioutil.ReadAll(resp.Body)
// 	return body, err
// }
//
// func postOGURL(command string, args map[string]string, body []byte) bool {
// 	u, _ := url.Parse(ogURLPrefix + "/" + scheduleID + "/" + command)
// 	cli := &http.Client{}
// 	req, _ := http.NewRequest("POST", u.String(), bytes.NewBuffer(body))
// 	req.Header.Add("Authorization", authKey)
// 	req.Header.Add("Content-Type", "application/json")
// 	resp, err := cli.Do(req)
// 	if err != nil {
// 		panic(err)
// 	}
// 	defer resp.Body.Close()
// 	fmt.Println("response Status:", resp.Status)
// 	fmt.Println("response Headers:", resp.Header)
// 	returnbody, _ := ioutil.ReadAll(resp.Body)
// 	fmt.Println("response Body:", string(returnbody))
// 	return true
// }
