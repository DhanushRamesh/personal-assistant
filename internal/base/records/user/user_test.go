package user_test

import (
	"errors"
	"strings"
	"testing"

	"personal-assistant/internal/base/records/user"
)

// TestAUsernameIsNarrowedBeforeItIsJudged : Trimmed and lowered
// first, so the same person typing the same name two ways is the same
// person.
//
// It matters more than it looks: the username is what somebody logs
// in with, and two accounts differing only in a capital letter or a
// trailing space is a way in that nobody intended.
func TestAUsernameIsNarrowedBeforeItIsJudged(t *testing.T) {
	for _, given := range []string{"Dhanush", "  dhanush  ", "DHANUSH", "\tDhanush\n"} {
		made, err := user.NewUser(given, "a-hash")
		if err != nil {
			t.Fatalf("%q was refused: %v", given, err)
		}
		if made.Username != "dhanush" {
			t.Errorf("%q became %q, want it narrowed to dhanush", given, made.Username)
		}
		if user.NormaliseUsername(given) != made.Username {
			t.Errorf("NormaliseUsername(%q) disagrees with what NewUser stored", given)
		}
	}
}

// TestWhatAUsernameMayNotBe : The pattern is deliberately narrow, and
// a narrow rule is only worth having if it is kept.
func TestWhatAUsernameMayNotBe(t *testing.T) {
	for _, c := range []struct{ what, username string }{
		{"empty", ""},
		{"one character", "d"},
		{"longer than the limit", strings.Repeat("d", user.MaxUsernameLen+1)},
		{"starting with punctuation", ".dhanush"},
		{"starting with a dash", "-dhanush"},
		{"with a space inside", "dha nush"},
		{"with an at sign", "dhanush@home"},
		{"with a slash", "dhanush/1"},
		{"not latin", "தனுஷ்"},
	} {
		t.Run(c.what, func(t *testing.T) {
			if _, err := user.NewUser(c.username, "a-hash"); !errors.Is(err, user.ErrInvalidUsername) {
				t.Errorf("%q was accepted (err = %v)", c.username, err)
			}
		})
	}

	// And the edges of the limit, which is where an off-by-one lives.
	for _, ok := range []string{
		strings.Repeat("d", user.MinUsernameLen),
		strings.Repeat("d", user.MaxUsernameLen),
		"d1", "d.n", "d_n", "d-n", "0dhanush",
	} {
		if _, err := user.NewUser(ok, "a-hash"); err != nil {
			t.Errorf("%q was refused: %v", ok, err)
		}
	}
}

// TestAUserWithoutAPasswordHashIsRefused : Storing one would be an
// account that cannot be logged into and cannot be told from one that
// can.
func TestAUserWithoutAPasswordHashIsRefused(t *testing.T) {
	if _, err := user.NewUser("dhanush", ""); err == nil {
		t.Fatal("a user with no password hash was created")
	}
}

// TestAnIdentifierSaysWhatItBelongsTo : The prefix is how an
// identifier is recognisable wherever it turns up, so it is worth
// checking that what is minted is what is accepted.
func TestAnIdentifierSaysWhatItBelongsTo(t *testing.T) {
	id := user.NewUserID()
	if !strings.HasPrefix(id, user.UserIDPrefix) {
		t.Errorf("%q does not begin with %q", id, user.UserIDPrefix)
	}
	if !user.ValidUserID(id) {
		t.Errorf("%q was minted and then refused", id)
	}
	if user.NewUserID() == id {
		t.Error("two identifiers came out the same")
	}

	for _, bad := range []string{"", "usr_", "usr_short", id[4:], "cli_" + id[4:], id + "x"} {
		if user.ValidUserID(bad) {
			t.Errorf("%q was accepted as a user identifier", bad)
		}
	}
}

// TestWhatIsStoredIsWhatWasMade : The timestamps go into DATETIME(3)
// columns, so a value kept at nanoseconds stops matching its own row.
func TestWhatIsStoredIsWhatWasMade(t *testing.T) {
	made, err := user.NewUser("dhanush", "a-hash")
	if err != nil {
		t.Fatalf("NewUser: %v", err)
	}
	if made.CreatedAt.Truncate(1e6) != made.CreatedAt {
		t.Errorf("CreatedAt = %v, want it truncated to the millisecond", made.CreatedAt)
	}
	if !made.UpdatedAt.Equal(made.CreatedAt) {
		t.Error("a user was made already out of date")
	}
}
