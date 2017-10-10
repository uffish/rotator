package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
)

func dateFormat(day time.Time) string {
	timeParse := "2006-01-02"
	return day.Format(timeParse)
}

// getClient uses a Context and Config to retrieve a Token
// then generate a Client. It returns the generated Client.
func getClient(ctx context.Context, config *oauth2.Config) *http.Client {
	cacheFile, err := tokenCacheFile()
	if err != nil {
		log.Fatalf("Unable to get path to cached credential file. %v", err)
	}
	tok, err := tokenFromFile(cacheFile)
	if err != nil {
		tok = getTokenFromWeb(config)
		saveToken(cacheFile, tok)
	}
	return config.Client(ctx, tok)
}

func getDayEvents(srv *calendar.Service, day time.Time) ([]*calendar.Event, error) {
	starttime := day.Truncate(time.Hour * 24).Add(time.Second)
	endtime := starttime.Add(time.Minute)
	// calendars, err := srv.Calendars.List
	events, err := srv.Events.List(config.AvailabilityCalendar).
		SingleEvents(true).
		TimeMax(endtime.Format(time.RFC3339)).
		TimeMin(starttime.Format(time.RFC3339)).
		OrderBy("startTime").Do()
	return events.Items, err
}

func getMonthRange(dayOne time.Time, dayCount int) (time.Time, int) {
	firstDay := time.Date(dayOne.Year(), dayOne.Month(), 1, 0, 0, 0, 0, time.UTC)
	lastDay := dayOne.AddDate(0, 0, dayCount)
	lastMonthDay := time.Date(lastDay.Year(), lastDay.Month()+1, 0, 0, 0, 0, 0, time.UTC)
	return firstDay, int(lastMonthDay.Sub(firstDay) / (time.Hour * 24))
}

func getOncallMonthRestrictions(srv *calendar.Service, month time.Time) map[string]*restriction {
	res := make(map[string]*restriction)
	res[oncallerShadow.Code] = &restriction{-31, -31}

	for _, person := range config.Oncallers {
		res[person.Code] = &restriction{0, 0}
	}

	if *flagUnrestrict {
		// recast the schedule from scratch, so return all zeeeeeeeroes.
		return res
	}

	// FIXME horrible time zone handling here - needs a general solution
	firstday := time.Date(month.Year(), month.Month(), 1, 3, 0, 0, 0, time.UTC)
	// why does this work? because day 0 of a month is the last day of month-1!
	daysinmonth := time.Date(month.Year(), month.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
	for day := 0; day < daysinmonth; day++ {
		nextday := firstday.AddDate(0, 0, day)
		oncall := oncall.Days[dateFormat(nextday)]
		if oncall.Victim.Code == "" {
			continue
		}

		res[oncall.Victim.Code].DaysBooked++
		if isWeekend(nextday) {
			res[oncall.Victim.Code].WeekendsBooked++
			// if day 1 is a Sunday, add 2 to avoid off-by-one errors later on...
			if nextday.Weekday() == 0 && nextday.Day() == 1 {
				res[oncall.Victim.Code].WeekendsBooked++
			}
		}
		// if *Verbose {
		// 	fmt.Printf("Day: %d/%d Victim: %s WE: %t\n", day+1, weekday, oncall.Victim, isWeekend(firstday.AddDate(0, 0, day)))
		// }
	}
	if *flagDebug {
		fmt.Printf("Oncall restrictions for %s %d:\n", month.Month(), month.Year())
		for v, r := range res {
			fmt.Printf("Oncaller: %s Days: %d WE: %d\n", v, r.DaysBooked, r.WeekendsBooked)
		}
	}
	return res
}

// Find the person in the oncall calendar for a given day.
func getOncallByDay(srv *calendar.Service, day time.Time) *oncallDay {

	oncallRe := regexp.MustCompile(`(?i)(\w{2,3}).*onduty(-fix)?`)
	fixed := false
	starttime := day.Truncate(time.Hour * 24).Add(time.Second)
	endtime := starttime.Add(time.Minute)
	// calendars, err := srv.Calendars.List
	events, err := srv.Events.List(config.OncallCalendar).
		SingleEvents(true).
		TimeMax(endtime.Format(time.RFC3339)).
		TimeMin(starttime.Format(time.RFC3339)).
		OrderBy("startTime").Do()
	if err != nil {
		log.Fatalf("Couldn't get entries from oncall calendar: %s\n", err)
	}
	if len(events.Items) > 0 {
		for _, event := range events.Items {
			if event.Start.DateTime == "" {
				title := event.Summary
				match := oncallRe.FindStringSubmatch(title)
				if match == nil {
					continue
				} else {
					if match[2] != "" {
						fixed = true
					}
					return &oncallDay{oncallersByCode[strings.ToLower(match[1])], fixed}
				}
			}
		}
	}
	// If nobody was oncall..
	return &oncallDay{oncallPerson{}, false}
}

// getTokenFromWeb uses Config to request a Token.
// It returns the retrieved Token.
func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the "+
		"authorization code: \n%v\n", authURL)

	var code string
	if _, err := fmt.Scan(&code); err != nil {
		log.Fatalf("Unable to read authorization code %v", err)
	}

	tok, err := config.Exchange(oauth2.NoContext, code)
	if err != nil {
		log.Fatalf("Unable to retrieve token from web %v", err)
	}
	return tok
}

func isWeekend(day time.Time) bool {
	if day.Weekday() == 0 || day.Weekday() == 6 {
		return true
	}
	return false
}

func makeAttendees(people []oncallPerson) []*calendar.EventAttendee {
	var attendees []*calendar.EventAttendee
	for _, person := range people {
		hasmail, _ := regexp.MatchString(`\@`, person.CalendarEmail)
		if hasmail == true {
			attendee := calendar.EventAttendee{
				Email: person.CalendarEmail,
			}
			attendees = append(attendees, &attendee)
		}
	}
	return attendees
}

func setOncallByDay(srv *calendar.Service, day time.Time, victim oncallPerson) bool {
	// Get existing oncall for day

	existingOncall := victim

	existing := oncall.Days[dateFormat(day)]
	if existing.Victim == existingOncall || existing.Fixed == true {
		// Nothing to do except increment their load counter if we reset it
		if *flagUnrestrict == true && existing.Fixed == false {
			restrictions.Detail[victim.Code].DaysBooked++
			if isWeekend(day) {
				restrictions.Detail[victim.Code].WeekendsBooked++
			}
		}
		return true
	}

	// otherwise we need to rewrite it.
	oncallRe := regexp.MustCompile(`(?i)(\w{2,3}).*onduty`)
	starttime := day.Truncate(time.Hour * 24)
	endtime := starttime.Add(time.Minute)

	events, err := srv.Events.List(config.OncallCalendar).
		SingleEvents(true).
		TimeMax(endtime.Format(time.RFC3339)).
		TimeMin(starttime.Format(time.RFC3339)).
		OrderBy("startTime").Do()
	if err != nil {
		log.Fatalf("Couldn't get entries from oncall calendar: %s\n", err)
	}
	rewritten := false
	if len(events.Items) > 0 {
		for _, event := range events.Items {
			title := event.Summary
			match := oncallRe.FindStringSubmatch(title)
			if match == nil {
				continue
			} else {
				eventAttendees := makeAttendees([]oncallPerson{victim})
				event.Attendees = eventAttendees
				event.Summary = fmt.Sprintf("%s onduty", existingOncall.Code)
				if *flagDryRun == false {
					_, err := srv.Events.Update(config.OncallCalendar, event.Id, event).Do()
					if err != nil {
						log.Fatalf("Event update failed: %s\n", err)
					}
				}
				if *flagVerbose {
					fmt.Printf("%s is now oncall on %s (was %s)\n", victim.Code,
						day.Format("2006-01-02"),
						existing.Victim.Code)
				}
				rewritten = true
			}
		}
	}
	if rewritten == false {
		eventAttendees := makeAttendees([]oncallPerson{victim})
		newEvent := calendar.Event{
			Attendees: eventAttendees,
			Summary:   fmt.Sprintf("%s onduty", existingOncall.Code),
			Start:     &calendar.EventDateTime{Date: starttime.Format("2006-01-02")},
			End:       &calendar.EventDateTime{Date: starttime.AddDate(0, 0, 1).Format("2006-01-02")},
		}
		if *flagDryRun == false {
			_, err := srv.Events.Insert(config.OncallCalendar, &newEvent).Do()
			if err != nil {
				fmt.Println(err)
				return false
			}
		}
	}
	// Increment the load counter..
	restrictions.Detail[victim.Code].DaysBooked++
	if isWeekend(day) {
		restrictions.Detail[victim.Code].WeekendsBooked++
	}
	// And decrement it if it was rewritten.
	if rewritten {
		restrictions.Detail[existing.Victim.Code].DaysBooked--
		if isWeekend(day) {
			restrictions.Detail[existing.Victim.Code].WeekendsBooked--
		}
	}
	return true
}

// tokenCacheFile generates credential file path/filename.
// It returns the generated credential path/filename.
func tokenCacheFile() (string, error) {
	usr, err := user.Current()
	if err != nil {
		return "", err
	}
	tokenCacheDir := filepath.Join(usr.HomeDir, ".credentials")
	os.MkdirAll(tokenCacheDir, 0700)
	return filepath.Join(tokenCacheDir,
		url.QueryEscape("calendar-go.json")), err
}

// tokenFromFile retrieves a Token from a given file path.
// It returns the retrieved Token and any read error encountered.
func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	t := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(t)
	defer f.Close()
	return t, err
}

// saveToken uses a file path to create a file and store the
// token in it.
func saveToken(file string, token *oauth2.Token) {
	fmt.Printf("Saving credential file to: %s\n", file)
	f, err := os.Create(file)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

func initCalendar(secretfile string) (*calendar.Service, error) {
	ctx := context.Background()

	b, err := ioutil.ReadFile(secretfile)
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}

	// If modifying these scopes, delete your previously saved credentials
	// at ~/.credentials/calendar-go-quickstart.json
	config, err := google.ConfigFromJSON(b, calendar.CalendarScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}
	client := getClient(ctx, config)

	srv, err := calendar.New(client)
	return srv, err
}
