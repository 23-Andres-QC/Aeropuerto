package domain

import (
	"math"
	"sort"
	"time"
)

// Matching of purchases with people. The cameras tell who was inside a local
// and for how long (ENTER and EXIT events of its INTERIOR zone); the register
// tells what was sold, where and when. A ticket belongs to the person whose
// stay in that local covers the moment of the sale, or ended just before it
// (the clocks of the register and the cameras never agree exactly).
//
// It is an inference, not a certainty, so every match keeps its confidence:
// the closer the sale is to the stay (and the later within it, since people
// pay before leaving) and the fewer other people could have made it, the
// higher it is. A person pays at most once per visit and a ticket has at most
// one person.

// MatchParams tune the matching.
type MatchParams struct {
	// SkewS is how far, in seconds, the register clock may differ from the
	// cameras': a sale this far outside a stay is still plausible.
	SkewS float64
	// MinScore discards candidates less likely than this.
	MinScore float64
}

// DefaultMatchParams are the ones the API uses.
var DefaultMatchParams = MatchParams{SkewS: 30, MinScore: 0.15}

// StayEvent is an ENTER or EXIT of a person in the INTERIOR zone of a local.
type StayEvent struct {
	GlobalID   string
	LocalID    int
	Type       string
	At         time.Time
	Confidence float64
}

// PersonSpan is when the model saw a person and who they are in the session.
type PersonSpan struct {
	Number    int
	Gender    string
	FirstSeen time.Time
	LastSeen  time.Time
}

// Stay is one visit of a person to a local.
type Stay struct {
	GlobalID   string
	Person     int
	Gender     string
	LocalID    int
	Start, End time.Time
	Confidence float64
	// Open: the trajectory ended inside the local, so End is the last time they were seen.
	Open bool
}

// PairStays turns ENTER/EXIT events into stays. Events must come ordered by
// person, local and time. An ENTER with no EXIT closes when the person was last seen.
func PairStays(events []StayEvent, people map[string]PersonSpan) []Stay {
	var stays []Stay
	var open *Stay
	// closeOpen ends the open stay at `until` (a new ENTER) or, with a zero
	// time, when the person was last seen.
	closeOpen := func(until time.Time) {
		if open == nil {
			return
		}
		switch p := people[open.GlobalID]; {
		case !until.IsZero():
			open.End = until
		case p.LastSeen.After(open.Start):
			open.End = p.LastSeen
		default:
			open.End = open.Start
		}
		open.Open = true
		stays = append(stays, *open)
		open = nil
	}
	for _, e := range events {
		if open != nil && (open.GlobalID != e.GlobalID || open.LocalID != e.LocalID) {
			closeOpen(time.Time{})
		}
		switch e.Type {
		case "ENTER":
			closeOpen(e.At)
			p := people[e.GlobalID]
			open = &Stay{GlobalID: e.GlobalID, Person: p.Number, Gender: p.Gender, LocalID: e.LocalID, Start: e.At, Confidence: e.Confidence}
		case "EXIT":
			if open != nil && !e.At.Before(open.Start) {
				open.End = e.At
				stays = append(stays, *open)
				open = nil
			}
		}
	}
	closeOpen(time.Time{})
	sort.SliceStable(stays, func(i, j int) bool { return stays[i].Start.Before(stays[j].Start) })
	return stays
}

// MatchTicket is the part of a ticket the matching needs.
type MatchTicket struct {
	ID      string
	LocalID int
	SoldAt  time.Time
}

// Match is a ticket assigned to a stay.
type Match struct {
	TicketID   string
	Stay       Stay
	OffsetS    float64
	Score      float64
	Confidence float64
}

// MatchResult is the outcome of matching a session's stays with the tickets of its time window.
type MatchResult struct {
	Matches []Match
	// Unmatched are the tickets nobody in the cameras explains (people the model did not see, or sales outside the stays).
	Unmatched []string
	// Stays and WithoutPurchase count the visits and the ones that bought nothing.
	Stays, WithoutPurchase int
}

// StayScore is how plausible it is that the person of a stay made the sale at t, from 0 to 1.
func StayScore(s Stay, t time.Time, p MatchParams) float64 {
	var distance float64
	inside := !t.Before(s.Start) && !t.After(s.End)
	switch {
	case t.Before(s.Start):
		distance = s.Start.Sub(t).Seconds()
	case t.After(s.End):
		distance = t.Sub(s.End).Seconds()
	}
	if distance > 3*p.SkewS {
		return 0
	}
	closeness := math.Exp(-0.5 * (distance / p.SkewS) * (distance / p.SkewS))
	// Outside the stay: a sale just after leaving is natural (a register by the door), one before the
	// person came in can only be explained by the clocks.
	position := 0.6
	if t.Before(s.Start) {
		position = 0.35
	}
	if inside {
		if span := s.End.Sub(s.Start).Seconds(); span > 0 {
			position = 0.6 + 0.4*t.Sub(s.Start).Seconds()/span
		} else {
			position = 0.8
		}
	}
	return closeness * position * (0.5 + 0.5*clamp01(s.Confidence))
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

// MatchTickets assigns tickets to stays: the likeliest pairs first, one ticket
// per stay and one stay per ticket.
func MatchTickets(stays []Stay, tickets []MatchTicket, p MatchParams) MatchResult {
	type pair struct {
		ticket, stay int
		score        float64
	}
	var pairs []pair
	best := make([]map[int]float64, len(tickets)) // per ticket, the score of each candidate stay
	for ti, t := range tickets {
		for si, s := range stays {
			if s.LocalID != t.LocalID {
				continue
			}
			if score := StayScore(s, t.SoldAt, p); score >= p.MinScore {
				pairs = append(pairs, pair{ti, si, score})
				if best[ti] == nil {
					best[ti] = map[int]float64{}
				}
				best[ti][si] = score
			}
		}
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		if pairs[i].score != pairs[j].score {
			return pairs[i].score > pairs[j].score
		}
		if pairs[i].ticket != pairs[j].ticket {
			return tickets[pairs[i].ticket].SoldAt.Before(tickets[pairs[j].ticket].SoldAt)
		}
		return stays[pairs[i].stay].GlobalID < stays[pairs[j].stay].GlobalID
	})

	res := MatchResult{Stays: len(stays)}
	ticketDone, stayDone := make([]bool, len(tickets)), make([]bool, len(stays))
	for _, c := range pairs {
		if ticketDone[c.ticket] || stayDone[c.stay] {
			continue
		}
		ticketDone[c.ticket], stayDone[c.stay] = true, true
		// Ambiguity: another stay that could equally have made the sale halves the confidence.
		var rival float64
		for si, sc := range best[c.ticket] {
			if si != c.stay && sc > rival {
				rival = sc
			}
		}
		s := stays[c.stay]
		res.Matches = append(res.Matches, Match{
			TicketID:   tickets[c.ticket].ID,
			Stay:       s,
			OffsetS:    tickets[c.ticket].SoldAt.Sub(s.End).Seconds(),
			Score:      c.score,
			Confidence: c.score * c.score / (c.score + rival),
		})
	}
	sort.SliceStable(res.Matches, func(i, j int) bool {
		return res.Matches[i].Stay.Start.Before(res.Matches[j].Stay.Start)
	})
	for ti, t := range tickets {
		if !ticketDone[ti] {
			res.Unmatched = append(res.Unmatched, t.ID)
		}
	}
	for si := range stays {
		if !stayDone[si] {
			res.WithoutPurchase++
		}
	}
	return res
}
