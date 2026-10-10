package calling_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"personal-assistant/internal/base/world/calling"
)

func TestANameGoesToThePhoneAsItIs(t *testing.T) {
	var got string
	phone := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = string(body)
		io.WriteString(w, calling.Expected)
	}))
	defer phone.Close()

	err := calling.Phone{URL: phone.URL}.Ring(context.Background(),
		calling.Call{Who: "Alekhya Chintada"})
	if err != nil {
		t.Fatalf("ringing failed: %v", err)
	}
	if got != "Alekhya Chintada" {
		t.Errorf("the phone was sent %q", got)
	}
}

// A captive portal's cheerful 200 is not a call having been placed.
func TestSomethingOtherThanThePhoneIsNotACall(t *testing.T) {
	phone := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "<html>sign in to continue</html>")
	}))
	defer phone.Close()

	err := calling.Phone{URL: phone.URL}.Ring(context.Background(), calling.Call{Who: "Amma"})
	if err == nil {
		t.Fatal("a page of HTML was taken for a call")
	}
}

func TestANumberIsNeverDialled(t *testing.T) {
	reached := false
	phone := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		io.WriteString(w, calling.Expected)
	}))
	defer phone.Close()

	for _, who := range []string{"+916746580844", "9876543210", "044 2851 2345"} {
		err := calling.Phone{URL: phone.URL}.Ring(context.Background(), calling.Call{Who: who})
		if err == nil {
			t.Errorf("%q was allowed through as a name", who)
		}
	}
	if reached {
		t.Error("a number reached the phone")
	}
}

// A name is one line. Anything carrying a second one would arrive as
// a name and a stray instruction.
func TestANameIsOneLine(t *testing.T) {
	got, err := calling.Body(calling.Call{Who: " Vel\nKumara  "})
	if err != nil {
		t.Fatalf("that name was refused: %v", err)
	}
	if got != "Vel Kumara" {
		t.Errorf("the name went as %q", got)
	}
}

func TestNobodyToRingIsRefused(t *testing.T) {
	if _, err := calling.Body(calling.Call{Who: "   "}); err == nil {
		t.Error("an empty name was allowed")
	}
}

func TestWithNoAddressItSaysSo(t *testing.T) {
	err := calling.Phone{}.Ring(context.Background(), calling.Call{Who: "Amma"})
	if err == nil || !strings.Contains(err.Error(), "no address") {
		t.Errorf("with nowhere to ring it said: %v", err)
	}
}

// A name with a digit in it -- a contact saved as "Ramesh 2" -- is a
// name, not a number.
func TestANameWithANumberInItIsStillAName(t *testing.T) {
	if calling.Digits("Ramesh 2") {
		t.Error("a contact with a digit in the name was taken for a number")
	}
}
