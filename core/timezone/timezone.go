// Package timezone loads named time zones and converts instants between them.
package timezone

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"
)

// ErrInvalidZone identifies a missing, invalid, or unavailable time zone.
var ErrInvalidZone = errors.New("timezone: invalid or unavailable zone")

// Load resolves an explicit IANA zone identifier using Go's time zone data sources.
// UTC is accepted. Host aliases such as Local are rejected.
func Load(zoneID string) (*time.Location, error) {
	if !fs.ValidPath(zoneID) || zoneID == "." || strings.Contains(zoneID, `\`) ||
		strings.EqualFold(zoneID, "Local") || strings.EqualFold(zoneID, "localtime") || strings.EqualFold(zoneID, "posixrules") {
		return nil, fmt.Errorf("%w: %q", ErrInvalidZone, zoneID)
	}
	location, err := time.LoadLocation(zoneID)
	if err != nil {
		return nil, fmt.Errorf("%w: %q: %w", ErrInvalidZone, zoneID, err)
	}
	return location, nil
}

// Convert represents instant in the named zone while preserving the instant.
// For repeated conversions in one zone, Load once and use time.Time.In.
func Convert(instant time.Time, zoneID string) (time.Time, error) {
	location, err := Load(zoneID)
	if err != nil {
		return time.Time{}, err
	}
	return instant.In(location), nil
}
