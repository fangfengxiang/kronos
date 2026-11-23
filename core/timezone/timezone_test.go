package timezone_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/core/timezone"
)

func TestConvertInDifferentZones(t *testing.T) {
	instant := time.Date(2025, time.January, 2, 1, 0, 0, 123456789, time.UTC)
	for _, test := range []struct {
		zoneID string
		want   string
	}{
		{zoneID: "UTC", want: "2025-01-02T01:00:00.123456789Z"},
		{zoneID: "America/Los_Angeles", want: "2025-01-01T17:00:00.123456789-08:00"},
		{zoneID: "Asia/Shanghai", want: "2025-01-02T09:00:00.123456789+08:00"},
		{zoneID: "Asia/Kathmandu", want: "2025-01-02T06:45:00.123456789+05:45"},
		{zoneID: "Pacific/Kiritimati", want: "2025-01-02T15:00:00.123456789+14:00"},
	} {
		t.Run(test.zoneID, func(t *testing.T) {
			localTime, err := timezone.Convert(instant, test.zoneID)
			if err != nil {
				t.Fatal(err)
			}
			if got := localTime.Format(time.RFC3339Nano); got != test.want {
				t.Errorf("local time = %s; want %s", got, test.want)
			}
			if !localTime.Equal(instant) || localTime.Location().String() != test.zoneID {
				t.Errorf("conversion changed the instant or selected the wrong zone: %v (%s)", localTime, localTime.Location())
			}
		})
	}
}

func TestLoadDaylightSavingTransitions(t *testing.T) {
	location, err := timezone.Load("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		instant time.Time
		want    string
	}{
		{"before spring forward", time.Date(2025, time.March, 9, 6, 59, 0, 0, time.UTC), "2025-03-09T01:59:00-05:00"},
		{"after spring forward", time.Date(2025, time.March, 9, 7, 0, 0, 0, time.UTC), "2025-03-09T03:00:00-04:00"},
		{"first 01:30", time.Date(2025, time.November, 2, 5, 30, 0, 0, time.UTC), "2025-11-02T01:30:00-04:00"},
		{"second 01:30", time.Date(2025, time.November, 2, 6, 30, 0, 0, time.UTC), "2025-11-02T01:30:00-05:00"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.instant.In(location).Format(time.RFC3339); got != test.want {
				t.Errorf("local time = %s; want %s", got, test.want)
			}
		})
	}
}

func TestRejectInvalidZones(t *testing.T) {
	instant := time.Date(2025, time.January, 2, 1, 0, 0, 0, time.UTC)
	for _, zoneID := range []string{"", "Local", "localtime", "posixrules", ".", "../UTC", "Asia//Shanghai", `Asia\Shanghai`, "UTC+08:00", "Unknown/Zone"} {
		t.Run(zoneID, func(t *testing.T) {
			if location, err := timezone.Load(zoneID); location != nil || !errors.Is(err, timezone.ErrInvalidZone) {
				t.Errorf("Load(%q) = (%v, %v); want nil and ErrInvalidZone", zoneID, location, err)
			}
			if localTime, err := timezone.Convert(instant, zoneID); !localTime.IsZero() || !errors.Is(err, timezone.ErrInvalidZone) {
				t.Errorf("Convert(%q) = (%v, %v); want zero time and ErrInvalidZone", zoneID, localTime, err)
			}
		})
	}
}

func ExampleConvert() {
	completedAt := time.Date(2025, time.January, 2, 1, 0, 0, 0, time.UTC)
	for _, zoneID := range []string{"America/Los_Angeles", "Asia/Shanghai"} {
		localTime, err := timezone.Convert(completedAt, zoneID)
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println(zoneID, localTime.Format(time.DateOnly))
	}
	// Output:
	// America/Los_Angeles 2025-01-01
	// Asia/Shanghai 2025-01-02
}
