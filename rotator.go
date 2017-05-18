package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/uffish/holidays"
	"google.golang.org/api/calendar/v3"
	"gopkg.in/yaml.v1"
)

// Order: order in oncall sequence (0, 1, ...)
// Code: 2-3 letter identification code (usually initials)
// CalendarEmail: Google Calendar account email address
// Email: email address for notifications.
type oncallPerson struct {
	Order         int
	Code          string
	CalendarEmail string
	Email         string
}

type Config struct {
	SecretFile           string
	GenerateDays         int
	MailServer           string
	MailSender           string
	AvailabilityCalendar string
	OncallCalendar       string
	AwayWords            []string
	Oncallers            []oncallPerson
}

var config Config
var oncallersByCode map[string]oncallPerson
var oncallersByOrder map[int]oncallPerson
var holiday_re *regexp.Regexp

var (
	startDate    = flag.String("startdate", "", "Start date (YYYY-MM-DD) for rota generation")
	lastOn       = flag.String("laston", "", "Seed rota with yesterday's oncall person")
	generateDays = flag.Int("days", 0, "Number of days of rota to generate (overrides config file)")
	configFile   = flag.String("configfile", "rotator.yaml", "Where to look for config file")
	monitorFile  = flag.String("monitoring.file", "", "If set, write monitoring status to file and exit.")
	notifyVictim = flag.String("notify", "", "Send mail to whoever is oncall [today] or [tomorrow].")
	Verbose      = flag.Bool("v", false, "Print extra debugging information")
)

func init() {
	flag.Parse()

	cfgfile, _ := filepath.Abs(*configFile)
	yamlfile, err := ioutil.ReadFile(cfgfile)
	if err != nil {
		log.Panic(err)
	}

	err = yaml.Unmarshal(yamlfile, &config)
	if err != nil {
		log.Panic(err)
	}

	oncallersByOrder = make(map[int]oncallPerson)
	for _, person := range config.Oncallers {
		oncallersByOrder[person.Order] = person
	}

	oncallersByCode = make(map[string]oncallPerson)
	for _, person := range config.Oncallers {
		oncallersByCode[person.Code] = person
	}

	// If only OncallCalendar is specified, assume the same calendar should
	// be used for availability information.
	if config.OncallCalendar != "" && config.AvailabilityCalendar == "" {
		config.AvailabilityCalendar = config.OncallCalendar
	}

	var awaywords string
	if len(config.AwayWords) != 0 {
		awaywords = strings.Join(config.AwayWords, "|")
	} else {
		awaywords = "away|urlaub|krank|vacation|leave|familienzeit|za"
	}
	holiday_re = regexp.MustCompile(`(?i)(\w{2,3})[\s-]+(` + awaywords + `)`)
}

func checkAvailability(srv *calendar.Service, day time.Time) ([]string, error) {
	unavailable := []string{}
	events, err := getDayEvents(srv, day)

	if len(events) > 0 {
		for _, e := range events {
			// Only look for all-day events (these have no associated time, just a date)
			if e.Start.DateTime == "" {
				title := e.Summary
				match := holiday_re.FindStringSubmatch(title)
				if match == nil {
					continue
				}
				unavailable = append(unavailable, strings.ToLower(match[1]))
			}
		}
	}
	return unavailable, err
}

func findNextOncall(unavailable []string, last_oncall string,
	workday bool) oncallPerson {
	// find array index of last_oncall
	next_index := -1
	last_index := oncallersByCode[last_oncall].Order

	// If it's a holiday, default to the same person as yesterday
	if workday == false {
		last_index = (last_index - 1) % len(oncallersByOrder)
	}

	// Loop through candidates looking for the next person in the rotation
	// who's available
	for next_index < 0 {
		available := true
		last_index = (last_index + 1) % len(oncallersByOrder)
		candidate := oncallersByOrder[last_index]
		for _, i := range unavailable {
			if i == candidate.Code {
				available = false
			}
		}
		if available {
			next_index = last_index
		}
	}
	// And we're done.
	return oncallersByOrder[next_index]
}

func main() {

	time_parse := "2006-01-02"
	var start_date time.Time
	var last_oncall string

	if *startDate == "" {
		start_date = time.Now().AddDate(0, 0, -1)
	} else {
		start_date, _ = time.Parse(time_parse, *startDate)
	}

	srv, err := initCalendar(config.SecretFile)
	if err != nil {
		log.Fatalf("Unable to initialise calendar client: %v", err)
	}
	// Stash today's oncaller for future reference (may be empty)
	oncaller := getOncallByDay(srv, time.Now()).Victim

	// Generate the monitoring file if that's all we need to do.
	if *monitorFile != "" {
		oncaller := getOncallByDay(srv, time.Now()).Victim
		err := writeMonitoringFile(oncaller, config.Oncallers, *monitorFile)
		if err != nil {
			fmt.Printf("Monitoring file creation failed: %s", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	srv_oncall := getOncallByDay(srv, start_date).Victim

	if *lastOn != "" {
		last_oncall = *lastOn
	} else if srv_oncall != "" {
		last_oncall = srv_oncall
	} else {
		last_oncall = oncallersByOrder[0].Code
	}

	// Default to 30 days unless overridden
	days := 30
	if *generateDays > 0 {
		days = *generateDays
	} else if config.GenerateDays != 0 {
		days = config.GenerateDays
	}

	for x := 1; x < days+1; x++ {
		today := start_date.AddDate(0, 0, x)
		workday := true
		hols := holidays.GetHolidays()

		if (holidays.CheckIsBusinessDay(today, hols) == false) &&
			(holidays.CheckIsBusinessDay(today.AddDate(0, 0, -1), hols) == false) {
			workday = false
		}

		unavailable, err := checkAvailability(srv, today)
		if err != nil {
			log.Fatalf("Unable to read calendar events: %v", err)
		}

		today_oncall := findNextOncall(unavailable, last_oncall, workday)
		if *Verbose == true {
			fmt.Printf("%s: %s # Out: %s\n",
				today.Format("Mon 2006-01-02"),
				today_oncall.Code,
				strings.Join(unavailable, ","))
		}
		setOncallByDay(srv, today, today_oncall)
		last_oncall = today_oncall.Code
	}

	// Check to see if today's oncaller has changed
	if oncaller != getOncallByDay(srv, time.Now()).Victim {
		// Notify the new oncaller
		err := doNotify(getOncallByDay(srv, time.Now()).Victim, "emergency")
		if err != nil {
			fmt.Printf("Error sending mail: %s\n", err)
		}
	}

	// Finally, notify current (or next) victim if required.
	var notifyresult error
	switch *notifyVictim {
	case "today":
		notifyresult = doNotify(getOncallByDay(srv, time.Now()).Victim, "today")
	case "tomorrow":
		notifyresult = doNotify(getOncallByDay(srv, time.Now().AddDate(0, 0, 1)).Victim, "tomorrow")
	}
	if notifyresult != nil {
		fmt.Printf("Error sending mail: %s\n", notifyresult)
	}

}
