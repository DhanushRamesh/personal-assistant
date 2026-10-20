package persona

import "testing"

// TestAYesIsAYesInHisLanguages : what he says, in the languages he
// says it in.
//
// Owner, 20 October 2026, stopping a regex being added: *"we can't
// handle these by regex!! what if I speak in another language"*.
// Measured then: Agrees was true of "yes" and false of "ஆம்", "சரி",
// "sí" and "हाँ". Worse than a miss -- the strip before the comparison
// was [^a-z' ], which deletes every non-Latin alphabet, so a Tamil yes
// reached the word list as the empty string.
//
// A word list works where it was written and nowhere else. These are
// English, Tamil and Hindi because those are his; a language nobody
// listed belongs to the prompt, which understands meaning rather than
// spelling.
func TestAYesIsAYesInHisLanguages(t *testing.T) {
	for _, said := range []string{
		"yes", "yeah", "ok", "okay", "sure", "go ahead", "do it",
		"ஆம்", "ஆமாம்", "சரி", "ஓகே", "செய்", "தொடரு",
		"हाँ", "हां", "जी", "ठीक है", "ओके", "करो", "जारी रखो",
	} {
		if !Agrees(said, "Jarvis") {
			t.Errorf("Agrees(%q) = false, and it is a yes", said)
		}
	}

	// The name and a hey around it come off in any alphabet.
	for _, said := range []string{"hey Jarvis ஆம்", "ஆம் Jarvis", "Jarvis हाँ"} {
		if !Agrees(said, "Jarvis") {
			t.Errorf("Agrees(%q) = false", said)
		}
	}

	// Still not a yes.
	for _, said := range []string{
		"no", "not now", "what is the time", "இல்லை", "नहीं",
		"add these to the list", "இவற்றைச் சேர்",
	} {
		if Agrees(said, "Jarvis") {
			t.Errorf("Agrees(%q) = true, and it is not a yes", said)
		}
	}
}

// TestSaidInPassingInHisLanguages : thanks and acknowledgements, which
// are answered with a word and never sent back to look at the tools.
func TestSaidInPassingInHisLanguages(t *testing.T) {
	for _, said := range []string{
		"thanks", "thank you", "got it", "hello", "bye",
		"நன்றி", "புரிஞ்சுது", "வணக்கம்", "பை",
		"धन्यवाद", "शुक्रिया", "समझ गया", "नमस्ते", "अलविदा",
	} {
		if !InPassing(said, "", "Jarvis") {
			t.Errorf("InPassing(%q) = false, and it was said in passing", said)
		}
	}

	// A question or an instruction is never in passing, however short.
	for _, said := range []string{
		"open Spotify", "what is the time", "ஸ்பாட்டிஃபை திற", "समय क्या है",
	} {
		if InPassing(said, "", "Jarvis") {
			t.Errorf("InPassing(%q) = true, and it asks for something", said)
		}
	}
}

// TestAnOfferIsAnOfferInHisLanguages : it answers in the language it
// was spoken to, so an offer it makes in Tamil has to be seen.
func TestAnOfferIsAnOfferInHisLanguages(t *testing.T) {
	for _, said := range []string{
		"The Server terminal is done, sir. Shall I tell it to keep going?",
		"சர்வர் முடிந்தது. அதைத் தொடரச் சொல்லவா?",
		"सर्वर हो गया। क्या मैं उसे आगे बढ़ने को कहूँ?",
	} {
		if !Offers(said) {
			t.Errorf("Offers(%q) = false, and it offers to act", said)
		}
	}

	// An honest question about what they meant is not an offer, in any
	// of them.
	for _, said := range []string{
		"The diary has two of these, sir. Did you mean the Tuesday one?",
		"டைரியில் இரண்டு உள்ளன. செவ்வாய் அன்று வருவதா?",
		"Told it to keep going, sir.",
	} {
		if Offers(said) {
			t.Errorf("Offers(%q) = true, and it only asks what was meant", said)
		}
	}
}
