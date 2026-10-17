// Package places keeps the places a person draws for themselves -- a
// name, a centre and a radius -- and works out when they cross into or
// out of one from the fixes their phone reports.
//
// Owner, 16 October 2026: *"instead of adding locations in Tasker, I
// should be able to map my geofencing in the server .. I can plot a
// point and set the radius and name that as location .. it should be
// added as geofence and not from Tasker"*.
//
// A crossing is written as the same place.entered / place.exited event
// Tasker used to send, with source "server". Everything that reads
// those -- rules, the greeting, the stays' names, the profile's rhythm
// -- needs nothing new. There is no table of who is inside where: the
// latest crossing the server wrote for a place is its state, so the
// record and the state cannot disagree.
package places

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// MaxName : The longest a place's name may be, in characters.
	MaxName = 60
	// MinRadius, MaxRadius : How small and how large a place may be
	// drawn, in metres.
	//
	// Twenty, the size of a shop, because a phone's own reading
	// scatters over about that much sitting still: smaller, and a fix
	// can fall outside it with them standing in it, so it is never
	// entered. Leaving takes Margin past the edge, so a small place is
	// not left by that scatter. It was fifty until the owner wanted a
	// supermarket (16 October 2026: *"I want less than 50 m"*). Two kilometres
	// because anything larger is a district, and being "in" it says
	// nothing worth a rule.
	MinRadius = 20
	MaxRadius = 2000
	// Margin : How far past a place's edge a fix must be before it
	// counts as having left, in metres.
	//
	// The edge itself is where readings wobble. Without a margin a
	// phone sitting by the gate enters and leaves every few minutes,
	// and every one of those is a rule firing and a stay cut in two.
	Margin = 50.0
	// SeedRadius : The radius Home and Office are given when they are
	// made from the stays already written there.
	SeedRadius = 150
	// Source : The source a crossing worked out here is written under.
	Source = "server"
)

// Seeded names : The two places made from what the stays already say,
// the first time somebody's places are asked for.
const (
	Home   = "Home"
	Office = "Office"
)

// Errors a caller can act on.
var (
	// ErrNotFound : No such place, or not theirs.
	ErrNotFound = errors.New("places: no such place")
	// ErrTaken : They already keep a place by that name.
	ErrTaken = errors.New("places: that name is already a place")
)

// Place : Somewhere a person drew.
type Place struct {
	// ID : This place.
	ID string
	// UserID : Whose it is.
	UserID string
	// Name : What it is called, and what a crossing reports.
	Name string
	// Lat, Lng : Its centre.
	Lat, Lng float64
	// Radius : How far from the centre is still inside, in metres.
	Radius int
	// CreatedAt, UpdatedAt : When it was drawn and last changed.
	CreatedAt, UpdatedAt time.Time
}

// Problems : Everything wrong with a place, all at once, so somebody
// fixing a form fixes it in one go.
type Problems []string

// Error : The problems as one sentence each.
func (p Problems) Error() string { return strings.Join(p, " ") }

// Clean : The place with its name trimmed, and what is wrong with it.
// Nil when nothing is.
func Clean(p Place) (Place, error) {
	p.Name = strings.TrimSpace(p.Name)
	var problems Problems
	switch n := utf8.RuneCountInString(p.Name); {
	case n == 0:
		problems = append(problems, "A place needs a name.")
	case n > MaxName:
		problems = append(problems, "That name is longer than 60 characters.")
	}
	if math.IsNaN(p.Lat) || p.Lat < -90 || p.Lat > 90 {
		problems = append(problems, "The latitude must be between -90 and 90.")
	}
	if math.IsNaN(p.Lng) || p.Lng < -180 || p.Lng > 180 {
		problems = append(problems, "The longitude must be between -180 and 180.")
	}
	if p.Radius < MinRadius || p.Radius > MaxRadius {
		problems = append(problems, fmt.Sprintf("The radius must be between %d and %d metres.", MinRadius, MaxRadius))
	}
	if len(problems) > 0 {
		return p, problems
	}
	return p, nil
}

// Same : Whether two names are the same place's, as a person would
// read them.
func Same(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// Store : Where places are kept.
type Store interface {
	// List : Everything somebody keeps, by name.
	List(ctx context.Context, userID string) ([]Place, error)
	// Get : One of theirs, or ErrNotFound.
	Get(ctx context.Context, userID, id string) (Place, error)
	// Add : Keeps a new one, or ErrTaken when the name is already
	// theirs, whatever its case.
	Add(ctx context.Context, p Place) error
	// Change : Rewrites one of theirs; ErrNotFound or ErrTaken.
	Change(ctx context.Context, p Place) error
	// Remove : Forgets one of theirs, or ErrNotFound.
	Remove(ctx context.Context, userID, id string) error
	// Seeded : Whether Home and Office have been made for them already.
	Seeded(ctx context.Context, userID string) (bool, error)
	// MarkSeeded : Records that they have, so deleting either does not
	// bring it back.
	MarkSeeded(ctx context.Context, userID string, at time.Time) error
}

// earth : The Earth's mean radius, in metres.
const earth = 6371000.0

// Metres : How far apart two positions are, along the ground.
func Metres(lat1, lng1, lat2, lng2 float64) float64 {
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLng := (lng2 - lng1) * rad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * earth * math.Asin(math.Min(1, math.Sqrt(h)))
}

// Inside : Whether a position is within the place.
func (p Place) Inside(lat, lng float64) bool {
	return Metres(p.Lat, p.Lng, lat, lng) <= float64(p.Radius)
}

// Outside : Whether a position is far enough out to have left it --
// past the edge by Margin.
func (p Place) Outside(lat, lng float64) bool {
	return Metres(p.Lat, p.Lng, lat, lng) > float64(p.Radius)+Margin
}
