package main

import (
	"fmt"
	"os"
	"path"
	"strings"
)

// Write a Prometheus-scrapeable file that tells us who's oncall.
func writeMonitoringFile(oncaller string,
	order []oncallPerson, dest string) error {
	hn, _ := os.Hostname()
	hn = strings.Split(hn, ".")[0]
	sn := path.Base(os.Args[0])
	// create or truncate the output file
	fd, err := os.Create(dest)
	if err != nil {
		return err
	}
	// neato function that executes the Close() either on function return or panic
	defer fd.Close()
	output := []string{}

	// print preamble
	output = append(output, fmt.Sprintf("# HELP oncall_rotation_status Positive if oncall."))
	output = append(output, fmt.Sprintf("# TYPE oncall_rotation_status gauge"))
	for _, person := range order {
		status := 0
		if oncaller == person.Code {
			status = 1
		}
		output = append(output, fmt.Sprintf(
			"oncall_rotation_status{scripthost=\"%s\",oncaller=\"%s\",scriptname=\"%s\"} %d",
			hn, person.Code, sn, status))
	}
	_, err = fd.WriteString(strings.Join(output, "\n") + "\n")
	return err
}
