// Package stored : The precision the database keeps time at.
//
// Here rather than in any one of the types that needs it, because
// chat, client, user and conversation all write timestamps into the
// same columns and the fact belongs to the columns rather than to any
// of them.
package stored

import "time"

// Precision : The precision timestamps are kept at.
//
// Go clocks to the nanosecond and MySQL's DATETIME(3) columns to the
// millisecond, rounding what it is given. Left alone, a value in
// memory stops matching the row it was just written to, and every
// later comparison between the two is subtly wrong. Truncating here
// rather than rounding means the value the domain holds is exactly
// the value that will be stored.
const Precision = time.Millisecond

// Now : The current time, at the precision it will be stored at.
func Now() time.Time { return time.Now().UTC().Truncate(Precision) }
