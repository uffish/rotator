package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/uffish/holidays"
	"github.com/uffish/holidays/austria"
	"google.golang.org/api/calendar/v3"
	"gopkg.in/yaml.v1"
)

// Config is mostly self-explanatory, although:
// MaxDaysPerMonth: Maximum days per month an individual may be oncall
// MaxWeekendsPerMonth: No more than this number of weekends/month/person
// ShadowOncaller: Will be listed as oncall if no oncaller can be found
// given the restrictions above - defaults to 'xx'
type Config struct {
	SecretFile           string
	GenerateDays         int
	MaxDaysPerMonth      int
	MaxWeekendsPerMonth  int
	MailServer           string
	MailSender           string
	AvailabilityCalendar string
	OncallCalendar       string
	SlackKey             string
	SlackChannel         string
	ShadowOncaller       string
	AwayWords            []string
	Oncallers            []oncallPerson
}

type oncallDay struct {
	Victim oncallPerson
	Fixed  bool
}

type oncallDaySet struct {
	Days map[string]*oncallDay
}

// Order: order in oncall sequence (0, 1, ...)
// Code: 2-3 letter identification code (usually initials)
// CalendarEmail: Google Calendar account email address
// Email: email address for notifications.
type oncallPerson struct {
	Order         int
	Code          string
	CalendarEmail string
	Email         string
	SlackID       string
}

type restriction struct {
	DaysBooked     int
	WeekendsBooked int
}

type restrictionSet struct {
	Month  time.Month
	Year   int
	Detail map[string]*restriction
}

var config Config
var oncallersByCode map[string]oncallPerson
var oncallersByOrder map[int]oncallPerson
var oncall oncallDaySet
var restrictions restrictionSet
var oncallerShadow oncallPerson
var holidayRE *regexp.Regexp

var (
	startDate      = flag.String("startdate", "", "Start date (YYYY-MM-DD) for rota generation")
	lastOn         = flag.String("laston", "", "Seed rota with yesterday's oncall person")
	generateDays   = flag.Int("days", 0, "Number of days of rota to generate (overrides config file)")
	configFile     = flag.String("configfile", "rotator.yaml", "Where to look for config file")
	monitorFile    = flag.String("monitoring.file", "", "If set, write monitoring status to file and exit.")
	notifyVictim   = flag.String("notify", "", "Send mail to whoever is oncall [today] or [tomorrow].")
	flagDebug      = flag.Bool("d", false, "Print spammy debugging information")
	flagVerbose    = flag.Bool("v", false, "Be a bit more verbose")
	flagDryRun     = flag.Bool("dry_run", true, "Don't actually write any calendar entries")
	flagUnrestrict = flag.Bool("unrestrict", false, "Start restrictions from zero (for recasting schedule)")
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

	if config.ShadowOncaller != "" {
		oncallerShadow.Code = config.ShadowOncaller
	} else {
		oncallerShadow.Code = "xx"
	}

	oncallersByOrder = make(map[int]oncallPerson)
	for _, person := range config.Oncallers {
		oncallersByOrder[person.Order] = person
	}

	oncallersByCode = make(map[string]oncallPerson)
	for _, person := range config.Oncallers {
		oncallersByCode[person.Code] = person
	}

	oncall.Days = make(map[string]*oncallDay)

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
	holidayRE = regexp.MustCompile(`(?i)(\w{2,3})[\s-]+(` + awaywords + `)`)
}

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

func main() {

	var firstDate time.Time
	var lastOncall oncallPerson

	if *startDate == "" {
		firstDate = time.Now()
	} else {
		firstDate, _ = time.Parse("2006-01-02", *startDate)
	}

	srv, err := initCalendar(config.SecretFile)
	if err != nil {
		log.Fatalf("Unable to initialise calendar client: %v", err)
	}
	// Stash today's oncaller for future reference (may be empty)
	oncall.Days[dateFormat(time.Now())] = getOncallByDay(srv, time.Now())
	todayOncaller := oncall.Days[dateFormat(time.Now())].Victim

	// Generate the monitoring file if that's all we need to do.
	if *monitorFile != "" {
		err := writeMonitoringFile(todayOncaller.Code, config.Oncallers, *monitorFile)
		if err != nil {
			fmt.Printf("Monitoring file creation failed: %s", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Default to 30 days unless overridden
	daysToRotate := 30
	if *generateDays > 0 {
		daysToRotate = *generateDays
	} else if config.GenerateDays != 0 {
		daysToRotate = config.GenerateDays
	}

	// Load the existing rotation in advance (we'll need it all anyway)
	firstOfMonth, daysToFetch := getMonthRange(firstDate, daysToRotate)

	if *flagDebug {
		fmt.Printf("Prefetching %d days...", daysToFetch)
	}
	for x := -1; x <= daysToFetch+1; x++ {
		day := firstOfMonth.AddDate(0, 0, x)
		oncall.Days[dateFormat(day)] = getOncallByDay(srv, day)
	}
	if *flagDebug {
		fmt.Printf("done\n")
	}

	// get day-1 oncall to prime the rotation

	if *lastOn != "" {
		lastOncall = oncallersByCode[*lastOn]
	} else if oncall.Days[dateFormat(firstDate.AddDate(0, 0, -1))].Victim.Code != "" {
		lastOncall = oncall.Days[dateFormat(firstDate.AddDate(0, 0, -1))].Victim
		if *flagDebug {
			fmt.Printf("Yesterday's oncall (starting point) was: %s\n", lastOncall.Code)
		}
	} else {
		lastOncall = oncallersByOrder[0]
	}

	for x := 0; x < daysToRotate; x++ {
		day := firstDate.AddDate(0, 0, x)
		workday := true
		hols := austria.GetHolidays()

		if (holidays.CheckIsBusinessDay(day, hols) == false) &&
			(holidays.CheckIsBusinessDay(day.AddDate(0, 0, -1), hols) == false) {
			workday = false
		}

		unavailable, err := checkAvailability(srv, day)
		if err != nil {
			log.Fatalf("Unable to read calendar events: %v", err)
		}

		// check to see if there's a fixed entry - if so, skip from here
		fixcheck := oncall.Days[dateFormat(day)]
		if fixcheck.Fixed == true {
			lastOncall = fixcheck.Victim
			if *flagVerbose == true {
				fmt.Printf("%s: %s # Fixed,Out: %s\n",
					day.Format("Mon 2006-01-02"),
					fixcheck.Victim.Code,
					strings.Join(unavailable, ","))
			}
			continue
		}

		dayOncall := findNextOncall(unavailable, lastOncall, workday)
		if *flagVerbose == true {
			fmt.Printf("%s: %s # Out: %s\n",
				day.Format("Mon 2006-01-02"),
				dayOncall.Code,
				strings.Join(unavailable, ","))
		}

		// FIXME unravel pointers
		oncall.Days[dateFormat(day)] = &oncallDay{dayOncall, false}
		setOncallByDay(srv, day, dayOncall)
		lastOncall = dayOncall
	}

	// Check to see if today's oncaller has changed
	if todayOncaller != oncall.Days[dateFormat(time.Now())].Victim {
		// Notify the new oncaller
		err := doNotify(oncall.Days[dateFormat(time.Now())].Victim, "emergency")
		if err != nil {
			fmt.Printf("Error sending mail: %s\n", err)
		}
	}

	// Finally, notify current (or next) victim if required.
	var notifyresult error
	switch *notifyVictim {
	case "today":
		notifyresult = doNotify(oncall.Days[dateFormat(time.Now())].Victim, "today")
	case "tomorrow":
		notifyresult = doNotify(oncall.Days[dateFormat(time.Now().AddDate(0, 0, 1))].Victim, "tomorrow")
	}
	if notifyresult != nil {
		fmt.Printf("Error sending mail: %s\n", notifyresult)
	}

}
