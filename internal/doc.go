// Package internal : Holds the server's own packages, in three layers.
//
// The layers are not a filing convention. They are the import graph,
// made visible: it was measured before the directories existed, and
// chat was imported by twenty-nine packages while importing none.
// That property is worth keeping and nothing but a test keeps it.
//
//   - base   -- the floor. chat, prompt, event, logging and the rest
//     of what everything leans on. Nothing here may import anything
//     above it, which is what makes it safe to change what is above.
//   - domain -- what the assistant knows about: reminders, rules, the
//     diary, the person's mail, and the stores that keep them.
//   - app    -- the wiring. The HTTP surface, the tools, the thing
//     that runs a turn. Everything may be reached from here and
//     nothing reaches back.
//
// layers_test.go is what enforces it. A package put straight into
// this directory belongs to no layer and is checked by nothing, so
// that fails too.
package internal
