package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/fangfengxiang/kronos/core/timezone"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil && !errors.Is(err, flag.ErrHelp) {
		log.Fatal(err)
	}
}

func run(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("reportdate", flag.ContinueOnError)
	zoneID := flags.String("zone", "", "IANA time zone")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("provide one RFC3339 instant")
	}
	instant, err := time.Parse(time.RFC3339, flags.Arg(0))
	if err != nil {
		return fmt.Errorf("parse report timestamp: %w", err)
	}
	localTime, err := timezone.Convert(instant, *zoneID)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, localTime.Format(time.DateOnly))
	return err
}
