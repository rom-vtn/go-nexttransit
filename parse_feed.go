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
const LOCATION_TIMEZONE = "Europe/Paris"

// func main() {
// 	if len(os.Args) < 4 {
// 		log.Fatalf("Usage: %s <gtfs-directory-path> <lat> <lon>", os.Args[0])
// 	}
// 	gtfsDirPath := os.Args[1]
// 	latString := os.Args[2]
// 	lonString := os.Args[3]
// 	lat, err := strconv.ParseFloat(latString, 64)
// 	if err != nil {
// 		panic(err)
// 	}
// 	lon, err := strconv.ParseFloat(lonString, 64)
// 	if err != nil {
// 		panic(err)
// 	}

// 	sights, err := GetNextBuses(lat, lon, gtfsDirPath)
// 	if err != nil {
// 		panic(err)
// 	}

// 	for _, sight := range sights {
// 		fmt.Printf("Bus: %s -> %s, at %s\n", sight.RouteName, sight.Headsign, sight.Timestamp.Format("15:04"))
// 	}
// }

type Sighting struct {
	Timestamp time.Time
	RouteName string
	Headsign  string
}

func GetNextBuses(lat, lon float64, dirPath string) ([]Sighting, error) {
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
		timestamp := today.Add(departureOffset)
		routeName := allRoutesMap[trip.RouteID].ShortName
		headsign := trip.Headsign
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
	location, _ := time.LoadLocation(LOCATION_TIMEZONE)
	fmt.Printf("location.String(): %v\n", location.String())
	println("AAAA")
	time, err := time.ParseInLocation("20060102", gtfsDayString, location)
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
	dayString := day.Format("20060102")
	weekday := day.Weekday()

	//get all service exceptions
	negativeExceptions := make(map[string]bool)
	var activeServices []string
	for _, calendarDate := range feed.CalendarDates {
		fmt.Printf("calendarDate: %v\n", calendarDate)
		if dayString != calendarDate.Date {
			continue
		}
		if calendarDate.ExceptionType == gtfs.ExceptionTypeAdded {
			activeServices = append(activeServices, calendarDate.ServiceID)
		} else {
			negativeExceptions[calendarDate.ServiceID] = true
		}
	}

	for _, calendar := range feed.Calendars {
		fmt.Printf("calendar: %v\n", calendar)
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
	fmt.Printf("negativeExceptions: %v\n", negativeExceptions)
	fmt.Printf("activeServices: %v\n", activeServices)
	return activeServicesMap
}
