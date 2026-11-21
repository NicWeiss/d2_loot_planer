package market

import (
	"math"
	"sort"
	"time"
)

const day = 24 * time.Hour

// settled: a realm without seasons is treated as a season this many days old, where prices barely move.
const settled = 365

// Relevance weighs a listing by how well its price still tells today's price.
//
// Prices slide fast after a season starts (on day one junk sells for Mal, a month later for a few Fal) and
// level off after a few months. So a listing's age is counted on a clock that runs fast early in the season
// and stops late in it, -1/(Pace + season day):
//
//	weight = exp(-Fade * (1/(Pace + day of the listing) - 1/(Pace + today)))
//
// A listing loses weight at Fade/(Pace + day)^2 per day. With Pace 7 and Fade 45: on day 1 a listing loses
// half its weight in about a day, on day 28 a week-old one keeps 0.7 and a day-0 one 0.006, on day 120 a
// month-old one keeps 0.9. Listings from before the season started are a different economy and get none.
// The zero Relevance weighs every listing 1.
type Relevance struct {
	Now         time.Time
	SeasonStart time.Time // zero: the realm has no seasons
	Pace        float64   // days
	Fade        float64   // days; 0 = no fading within the season
}

func (r Relevance) Off() bool { return r.Now.IsZero() }

// Day is the season day of t, counted from 0; a realm without seasons is always long into one.
func (r Relevance) Day(t time.Time) float64 {
	if r.SeasonStart.IsZero() {
		return settled - r.Now.Sub(t).Hours()/24
	}
	return t.Sub(r.SeasonStart).Hours() / 24
}

// Weight of a listing dated date (RFC 3339); 0 before the season, 1 for today or an unreadable date.
func (r Relevance) Weight(date string) float64 {
	if r.Off() {
		return 1
	}
	t, err := time.Parse(time.RFC3339, date)
	if err != nil {
		return 1
	}
	if t.After(r.Now) {
		t = r.Now
	}
	then, now := r.Day(t), r.Day(r.Now)
	if then < 0 {
		return 0
	}
	pace := math.Max(r.Pace, 0.5)
	return math.Exp(-r.Fade * (1/(pace+then) - 1/(pace+now)))
}

// DetectSeasonStart guesses when the current season of the given ladder value began: the first day with
// listings after the last quiet stretch (3+ days with under 5% of the recent daily volume). Zero when the
// listings show no such stretch, i.e. they all belong to the current season.
func DetectSeasonStart(ladder string, lists ...[]Ad) time.Time {
	perDay := map[time.Time]int{}
	for _, ads := range lists {
		for i := range ads {
			if ads[i].Ladder != ladder {
				continue
			}
			if t, err := time.Parse(time.RFC3339, ads[i].CreatedAt); err == nil {
				perDay[t.UTC().Truncate(day)]++
			}
		}
	}
	if len(perDay) == 0 {
		return time.Time{}
	}
	days := make([]time.Time, 0, len(perDay))
	for d := range perDay {
		days = append(days, d)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })
	first, last := days[0], days[len(days)-1]
	var recent []int // the last two weeks, gaps included
	for d := last.Add(-13 * day); !d.After(last); d = d.Add(day) {
		recent = append(recent, perDay[d])
	}
	sort.Ints(recent)
	quiet := float64(recent[len(recent)/2]) * 0.05
	run := 0
	for d := last; !d.Before(first); d = d.Add(-day) {
		if float64(perDay[d]) >= quiet {
			run = 0
			continue
		}
		if run++; run == 3 {
			for s := d.Add(3 * day); !s.After(last); s = s.Add(day) {
				if perDay[s] > 0 {
					return s
				}
			}
		}
	}
	return time.Time{}
}
