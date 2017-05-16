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

// Find the person in the oncall calendar for a given day.
func getOncallByDay(srv *calendar.Service, day time.Time) string {
	oncall_re := regexp.MustCompile(`(?i)(\w{2,3}).*onduty`)
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
				match := oncall_re.FindStringSubmatch(title)
				if match == nil {
					continue
				} else {
					return strings.ToLower(match[1])
				}
			}
		}
	}
	// If nobody was oncall..
	return ""
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

	oncall := victim.Code

	existing := getOncallByDay(srv, day)
	if existing == oncall {
		// Nothing to do!
		return true
	}
	// otherwise we need to rewrite it.
	oncall_re := regexp.MustCompile(`(?i)(\w{2,3}).*onduty`)
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
			match := oncall_re.FindStringSubmatch(title)
			if match == nil {
				continue
			} else {
				eventAttendees := makeAttendees([]oncallPerson{victim})
				event.Attendees = eventAttendees
				event.Summary = fmt.Sprintf("%s onduty", oncall)
				_, err := srv.Events.Update(config.OncallCalendar, event.Id, event).Do()
				if err != nil {
					log.Fatalf("Event update failed: %s\n", err)
				}
				if *Verbose == true {
					fmt.Printf("%s is now oncall on %s (was %s)\n", victim.Code,
						day.Format("2006-01-02"),
						existing)
				}
				rewritten = true
			}
		}
	}
	if rewritten == false {
		eventAttendees := makeAttendees([]oncallPerson{victim})
		newEvent := calendar.Event{
			Attendees: eventAttendees,
			Summary:   fmt.Sprintf("%s onduty", oncall),
			Start:     &calendar.EventDateTime{Date: starttime.Format("2006-01-02")},
			End:       &calendar.EventDateTime{Date: starttime.AddDate(0, 0, 1).Format("2006-01-02")},
		}
		_, err := srv.Events.Insert(config.OncallCalendar, &newEvent).Do()
		if err != nil {
			fmt.Println(err)
			return false
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
