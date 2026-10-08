package situation

import "time"

const (
	// Every : How often the checks are worth running.
	//
	// Not read here any more -- the schedule lives in the rule that
	// calls the tool -- and kept as the number that rule was made
	// with. Slower than the watcher that announces what a sender
	// marked, because nothing here is a warning from a device:
	// these are situations that have been true for a while by the
	// time they are worth saying, and will still be true in two
	// minutes.
	Every = 2 * time.Minute

	// Behind : How far back a run reads.
	//
	// Wide enough that a charger plugged in half an hour ago is
	// still in the window, since that is the whole of what the
	// charger check is about.
	Behind = 3 * time.Hour

	// Again : How long one situation stood said, when this package
	// remembered that for itself.
	//
	// It no longer does. A rule is told what it said last time and
	// asked whether what it has now is the same news, which covers
	// the thing this was for and covers the part it could not: the
	// quiet check's sentence says how long the silence has been, so
	// it was a different sentence every run and only the key kept
	// it quiet. Kept as a record of how long seemed right.
	Again = 6 * time.Hour
)
