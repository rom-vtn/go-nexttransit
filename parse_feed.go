package gonexttransit

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	gtfs "github.com/artonge/go-gtfs"
)

const KM_TO_DEGREES = 0.009
const DIST_THRESHOLD_KM = 0.3

type Sighting struct {
	Timestamp time.Time
	RouteName string
	Headsign  string
}

// returns the next bus sightings at the given location at the given day. Assumes that GTFS feed time = UTC + timezoneOffset
func GetNextBuses(lat, lon float64, dirPath string, day time.Time, timezoneOffset time.Duration) ([]Sighting, error) {
	feed, err := gtfs.Load(dirPath, nil)
	if err != nil {
		return nil, err
	}

	closeStopsMap := getCloseStops(feed, lat, lon)

	allTripsMap := make(map[string]gtfs.Trip)
	for _, trip := range feed.Trips {
		allTripsMap[trip.ID] = trip
	}
	allRoutesMap := make(map[string]gtfs.Route)
	for _, route := range feed.Routes {
		allRoutesMap[route.ID] = route
	}

	tripIdToTripHeadsign := getTripIdToHeadsign(feed)
	fmt.Printf("tripIdToTripHeadsign: %v\n", tripIdToTripHeadsign)

	today := time.Now().Truncate(24 * time.Hour)
	activeServicesMap := getActiveServicesOn(feed, today)

	var sights []Sighting
	for _, stopTime := range feed.StopsTimes {
		_, ok := closeStopsMap[stopTime.StopID]
		if !ok {
			continue
		}
		trip, ok := allTripsMap[stopTime.TripID]
		if !ok {
			panic("trip not part of all trips map")
		}
		if !activeServicesMap[trip.ServiceID] {
			continue
		}
		//do the actual sight
		departureOffset := timeDurationFromGtfsString(stopTime.Departure)
		timestamp := today.Add(departureOffset).Add(-timezoneOffset)
		routeName := allRoutesMap[trip.RouteID].ShortName
		headsign := tripIdToTripHeadsign[trip.ID]
		sight := Sighting{
			Timestamp: timestamp,
			RouteName: routeName,
			Headsign:  headsign,
		}
		sights = append(sights, sight)
	}

	sort.Slice(sights, func(i, j int) bool {
		return sights[i].Timestamp.Before(sights[j].Timestamp)
	})

	return sights, nil
}

// maps tripID to headsign
func getTripIdToHeadsign(feed *gtfs.GTFS) map[string]string {
	tripIdToLastStopId := make(map[string]string, len(feed.Trips))
	tripIdToMaxSequence := make(map[string]uint32, len(feed.Trips))
	for _, st := range feed.StopsTimes {
		maxSeq := tripIdToMaxSequence[st.TripID]
		if st.StopSeq <= maxSeq {
			continue
		}
		tripIdToLastStopId[st.TripID] = st.StopID
		tripIdToMaxSequence[st.TripID] = st.StopSeq
	}
	stopIdToStopName := make(map[string]string, len(feed.Stops))
	for _, stop := range feed.Stops {
		stopIdToStopName[stop.ID] = stop.Name
	}
	tripIdToTripHeadsign := make(map[string]string, len(feed.Trips))
	for tripId, lastStopId := range tripIdToLastStopId {
		lastStopName := stopIdToStopName[lastStopId]
		tripIdToTripHeadsign[tripId] = lastStopName
	}
	//then replace headsign if actually given
	for _, trip := range feed.Trips {
		if trip.Headsign != "" {
			tripIdToTripHeadsign[trip.ID] = trip.Headsign
		}
	}
	return tripIdToTripHeadsign
}

func getCloseStops(feed *gtfs.GTFS, wantedLat, wantedLon float64) map[string]gtfs.Stop {
	stops := make(map[string]gtfs.Stop)
	for _, stop := range feed.Stops {
		latDiff := (stop.Latitude - wantedLat) / KM_TO_DEGREES
		lonDiff := (stop.Longitude - wantedLon) / KM_TO_DEGREES
		distSquared := latDiff*latDiff + lonDiff*lonDiff
		if distSquared < DIST_THRESHOLD_KM*DIST_THRESHOLD_KM {
			stops[stop.ID] = stop
		}
	}
	return stops
}

// converts YYYYMMDD to time.Time
func dayFromFromGtfsString(gtfsDayString string) time.Time {
	time, err := time.Parse("20060102", gtfsDayString)
	if err != nil {
		panic(err)
	}
	return time
}

// converts hh:mm:ss to time.Duration
// note: gtfs times may go above 24:00:00 hence the need to do our own parsing
func timeDurationFromGtfsString(gtfsTimeString string) time.Duration {
	hourString := gtfsTimeString[0:2]
	minuteString := gtfsTimeString[3:5]
	secondString := gtfsTimeString[6:8]

	hour, err := strconv.ParseInt(hourString, 10, 64)
	if err != nil {
		panic(err)
	}
	minute, err := strconv.ParseInt(minuteString, 10, 64)
	if err != nil {
		panic(err)
	}
	second, err := strconv.ParseInt(secondString, 10, 64)
	if err != nil {
		panic(err)
	}
	return time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute + time.Duration(second)*time.Second
}

func isCalendarActiveOn(calendar gtfs.Calendar, weekday time.Weekday) bool {
	isDayActive := 0
	switch weekday {
	case time.Monday:
		isDayActive = calendar.Monday
	case time.Tuesday:
		isDayActive = calendar.Tuesday
	case time.Wednesday:
		isDayActive = calendar.Wednesday
	case time.Thursday:
		isDayActive = calendar.Thursday
	case time.Friday:
		isDayActive = calendar.Friday
	case time.Saturday:
		isDayActive = calendar.Saturday
	case time.Sunday:
		isDayActive = calendar.Sunday
	}
	return isDayActive == 1
}

func getActiveServicesOn(feed *gtfs.GTFS, moment time.Time) map[string]bool {
	day := moment.Truncate(24 * time.Hour)
	weekday := day.Weekday()

	//get all service exceptions
	negativeExceptions := make(map[string]bool)
	var activeServices []string
	for _, calendarDate := range feed.CalendarDates {
		if day.Format("20060102") != calendarDate.Date {
			continue
		}
		if calendarDate.ExceptionType == gtfs.ExceptionTypeAdded {
			activeServices = append(activeServices, calendarDate.ServiceID)
		} else {
			negativeExceptions[calendarDate.ServiceID] = true
		}
	}

	for _, calendar := range feed.Calendars {
		startDate := dayFromFromGtfsString(calendar.Start)
		endDate := dayFromFromGtfsString(calendar.End)
		if day.Before(startDate) || day.After(endDate) {
			continue
		}
		if isCalendarActiveOn(calendar, weekday) && !negativeExceptions[calendar.ServiceID] {
			activeServices = append(activeServices, calendar.ServiceID)
		}
	}

	activeServicesMap := make(map[string]bool)
	for _, serviceId := range activeServices {
		activeServicesMap[serviceId] = true
	}
	return activeServicesMap
}
