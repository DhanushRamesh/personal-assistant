// Package storetest runs one set of cases against any places.Store.
//
// The in-memory store stands in for MySQL in every API test, so the
// two must refuse the same things: a name kept twice in another case
// is the one most likely to drift, MySQL refusing it by collation and
// a map only if somebody remembered to.
package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"personal-assistant/internal/domain/places"
)

// New : Opens a store and returns it with two users to own places.
type New func(t *testing.T) (s places.Store, user, other string)

// Run : Every case both stores must pass.
func Run(t *testing.T, open New) {
	t.Helper()
	for _, c := range []struct {
		name string
		run  func(t *testing.T, open New)
	}{
		{"a place added comes back", anAddedOneComesBack},
		{"a name is theirs once whatever its case", aNameIsTakenOnce},
		{"another person may use the same name", anotherPersonMayShareAName},
		{"a place can be changed", aPlaceChanges},
		{"a rename onto another name is refused", aRenameOntoAnotherIsRefused},
		{"a place can be removed", aPlaceIsRemoved},
		{"somebody else's is not found", anotherPersonsIsNotFound},
		{"being seeded is remembered", seededIsRemembered},
	} {
		t.Run(c.name, func(t *testing.T) { c.run(t, open) })
	}
}

var at = time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)

func place(user, name string) places.Place {
	return places.Place{ID: places.NewID(), UserID: user, Name: name,
		Lat: 12.9108, Lng: 80.06215, Radius: 150, CreatedAt: at, UpdatedAt: at}
}

func anAddedOneComesBack(t *testing.T, open New) {
	ctx := context.Background()
	s, user, _ := open(t)
	p := place(user, "Home")
	if err := s.Add(ctx, p); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, err := s.Get(ctx, user, p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "Home" || got.Lat != p.Lat || got.Lng != p.Lng || got.Radius != 150 ||
		!got.CreatedAt.Equal(at) {
		t.Errorf("got %+v, want %+v", got, p)
	}
	all, err := s.List(ctx, user)
	if err != nil || len(all) != 1 || all[0].ID != p.ID {
		t.Errorf("List = %+v, %v", all, err)
	}
}

// "home" beside "Home" would be two places that every crossing and
// stay reads as one.
func aNameIsTakenOnce(t *testing.T, open New) {
	ctx := context.Background()
	s, user, _ := open(t)
	if err := s.Add(ctx, place(user, "Home")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Add(ctx, place(user, "HOME")); !errors.Is(err, places.ErrTaken) {
		t.Errorf("Add HOME = %v, want ErrTaken", err)
	}
}

func anotherPersonMayShareAName(t *testing.T, open New) {
	ctx := context.Background()
	s, user, other := open(t)
	if err := s.Add(ctx, place(user, "Home")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Add(ctx, place(other, "Home")); err != nil {
		t.Errorf("Add for another person = %v", err)
	}
}

func aPlaceChanges(t *testing.T, open New) {
	ctx := context.Background()
	s, user, _ := open(t)
	p := place(user, "Gym")
	if err := s.Add(ctx, p); err != nil {
		t.Fatalf("Add: %v", err)
	}
	p.Name, p.Radius, p.Lat, p.UpdatedAt = "The gym", 300, 12.95, at.Add(time.Hour)
	if err := s.Change(ctx, p); err != nil {
		t.Fatalf("Change: %v", err)
	}
	got, _ := s.Get(ctx, user, p.ID)
	if got.Name != "The gym" || got.Radius != 300 || got.Lat != 12.95 ||
		!got.UpdatedAt.Equal(at.Add(time.Hour)) || !got.CreatedAt.Equal(at) {
		t.Errorf("got %+v", got)
	}
	// Only its case changed: still its own name, not somebody else's.
	p.Name = "THE GYM"
	if err := s.Change(ctx, p); err != nil {
		t.Errorf("Change to its own name in capitals = %v", err)
	}
}

func aRenameOntoAnotherIsRefused(t *testing.T, open New) {
	ctx := context.Background()
	s, user, _ := open(t)
	home, office := place(user, "Home"), place(user, "Office")
	for _, p := range []places.Place{home, office} {
		if err := s.Add(ctx, p); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	office.Name = "home"
	if err := s.Change(ctx, office); !errors.Is(err, places.ErrTaken) {
		t.Errorf("Change = %v, want ErrTaken", err)
	}
}

func aPlaceIsRemoved(t *testing.T, open New) {
	ctx := context.Background()
	s, user, _ := open(t)
	p := place(user, "Home")
	if err := s.Add(ctx, p); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Remove(ctx, user, p.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := s.Get(ctx, user, p.ID); !errors.Is(err, places.ErrNotFound) {
		t.Errorf("Get after Remove = %v, want ErrNotFound", err)
	}
	if err := s.Remove(ctx, user, p.ID); !errors.Is(err, places.ErrNotFound) {
		t.Errorf("Remove twice = %v, want ErrNotFound", err)
	}
}

func anotherPersonsIsNotFound(t *testing.T, open New) {
	ctx := context.Background()
	s, user, other := open(t)
	p := place(user, "Home")
	if err := s.Add(ctx, p); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := s.Get(ctx, other, p.ID); !errors.Is(err, places.ErrNotFound) {
		t.Errorf("Get = %v", err)
	}
	q := p
	q.UserID, q.Name = other, "Mine now"
	if err := s.Change(ctx, q); !errors.Is(err, places.ErrNotFound) {
		t.Errorf("Change = %v", err)
	}
	if err := s.Remove(ctx, other, p.ID); !errors.Is(err, places.ErrNotFound) {
		t.Errorf("Remove = %v", err)
	}
	if all, _ := s.List(ctx, other); len(all) != 0 {
		t.Errorf("List for another person = %+v", all)
	}
}

func seededIsRemembered(t *testing.T, open New) {
	ctx := context.Background()
	s, user, other := open(t)
	if done, err := s.Seeded(ctx, user); err != nil || done {
		t.Fatalf("Seeded before = %v, %v", done, err)
	}
	for range 2 {
		if err := s.MarkSeeded(ctx, user, at); err != nil {
			t.Fatalf("MarkSeeded: %v", err)
		}
	}
	if done, _ := s.Seeded(ctx, user); !done {
		t.Error("not seeded after being marked")
	}
	if done, _ := s.Seeded(ctx, other); done {
		t.Error("another person was marked too")
	}
}
