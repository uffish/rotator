package main

import (
	"fmt"
  "io/ioutil"
  "log"
  "math/rand"
  "path/filepath"
	"strings"
	"time"

  "google.golang.org/api/calendar/v3"
  "gopkg.in/yaml.v1"
)

func checkAvailability(srv *calendar.Service, day time.Time) ([]string, error) {
	unavailable := []string{}
	overloaded := []string{}
	events, err := getDayEvents(srv, day)

	// this operation's expensive, so only fetch restriction data when we have to.
	if config.MaxDaysPerMonth+config.MaxWeekendsPerMonth > 0 {
		if restrictions.Month != day.Month() ||
			restrictions.Year != day.Year() {
			if *flagDebug {
				fmt.Printf("Fetching restriction info\n")
			}
			restrictions.Detail = getOncallMonthRestrictions(srv, day)
			restrictions.Month = day.Month()
			restrictions.Year = day.Year()
		}

		for k, v := range restrictions.Detail {
			// skip this if they're already oncall today, to avoid double-counting
			todayOncall := oncall.Days[dateFormat(day)]
			if todayOncall.Victim.Code == k {
				continue
			}
			if v.DaysBooked >= config.MaxDaysPerMonth ||
				(isWeekend(day) && v.WeekendsBooked >= config.MaxWeekendsPerMonth) {
				if *flagDebug {
					fmt.Printf("Oncaller overloaded: %s, %d/%d\n", k, v.DaysBooked, v.WeekendsBooked)
				}
				overloaded = append(overloaded, strings.ToLower(k))
				unavailable = append(unavailable, strings.ToLower(k))
			}
		}
	}
	if len(events) > 0 {
		for _, e := range events {
			// Only look for all-day events (these have no associated time, just a date)
			if e.Start.DateTime == "" {
				title := e.Summary
				match := holidayRE.FindStringSubmatch(title)
				if match == nil {
					continue
				}
				unavailable = append(unavailable, strings.ToLower(match[1]))
			}
		}
	}

	finallist := []string{}
	// remove any duplicates
	j := make(map[string]bool)
	for _, i := range unavailable {
		if !j[i] {
			j[i] = true
			finallist = append(finallist, i)
		}
	}
	return finallist, err
}

func findNextOncall(unavailable []string, lastOncall oncallPerson,
	workday bool) oncallPerson {
	var lastIndex int
	nextIndex := -1

	// find array index of lastOncall
	if lastOncall == oncallerShadow {
		// A random guess is probably as good as any..
		lastIndex = rand.Int() % len(oncallersByOrder)
	} else {
		lastIndex = oncallersByCode[lastOncall.Code].Order
	}

	// If it's a holiday, default to the same person as yesterday
	if workday == false {
		lastIndex = (lastIndex - 1) % len(oncallersByOrder)
	}

	if len(unavailable) == len(oncallersByOrder) {
		// uh-oh, nobody is available!
		return oncallerShadow
	}

	// Loop through candidates looking for the next person in the rotation
	// who's available
	for nextIndex < 0 {
		available := true
		lastIndex = (lastIndex + 1) % len(oncallersByOrder)
		candidate := oncallersByOrder[lastIndex]
		for _, i := range unavailable {
			if i == candidate.Code {
				available = false
			}
		}
		if available {
			nextIndex = lastIndex
		}
	}
	// And we're done.
	return oncallersByOrder[nextIndex]
}

func unpackConfig(fn string) Config {
  var c Config

  cfgfile, _ := filepath.Abs(fn)
  yamlfile, err := ioutil.ReadFile(cfgfile)
  if err != nil {
    log.Panic(err)
  }

  err = yaml.Unmarshal(yamlfile, &c)
  if err != nil {
    log.Panic(err)
  }
  return c
}
