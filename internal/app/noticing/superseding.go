package noticing

import "personal-assistant/internal/base/thinking/prompt"

// superseding : What the model is told when deciding whether a later
// memory has replaced an earlier one.
//
// Written to leave things alone. A person is allowed to be more than
// one thing, and most pairs that look contradictory are simply two
// facts: somebody can like two songs, live in one city and work in
// another, and have changed their mind about nothing at all.
var superseding = prompt.Lines(
	"These are the things you have been asked to remember about this person.",
	"Decide whether any of them has plainly been replaced by a later one.",
	"",
	"Almost always nothing has. Reply exactly this:",
	`  {"stands": "why they are all still true"}`,
	"",
	"Where one genuinely has been replaced, reply:",
	`  {"supersede": "<the id of the OLDER one, exactly as given>", "because": "<the later memory> says otherwise"}`,
	"",
	"Supersede one only where a later memory says something that cannot be true at the same time:",
	"A single thing that can only have one value -- where they live, where they work, what something is called -- stated differently later.",
	"Or a later memory that explicitly corrects the earlier one.",
	"",
	"Never supersede because two memories are merely different. Somebody can like two songs, know two people of the same name, and have two routines.",
	"Never supersede a preference just because a newer one exists. Liking a new artist does not stop them liking the old one, and a favourite is allowed to be one of several.",
	"Never supersede the standing description of them; it is rewritten every hour and looks after itself.",
	"Never supersede more than one. The single clearest, or none.",
	"If you are unsure, leave it. A memory kept too long is one wrong line; a memory removed wrongly is something they asked you to remember and you threw away.",
)
