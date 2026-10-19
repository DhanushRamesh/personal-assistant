package environment

// Directs : Whether the model picks its own next step for this purpose.
//
// The line between a workflow and an agent, which until 19 October 2026
// was in the right place by accident and nowhere in writing. Owner:
// *"jarvis can be both agentic and agent, but the roles should be
// clearly defined, only then it will be proper"*.
//
//   - A **workflow** follows a path written in code. One call, or a
//     fixed sequence of them, with the server deciding what comes next.
//     Predictable, and the reason it is predictable is that the model
//     is never asked what to do.
//   - An **agent** is handed tools and decides its own steps. Open-ended,
//     and worth it only where the number of steps cannot be known in
//     advance.
//
// Of 2,321 model calls in one week, 1,339 were the chat and 982 were
// the rest -- so 42% of the assistant's thinking is unasked-for work,
// and every bit of it is a workflow. That is what makes it
// predictable, and it is a property worth keeping on purpose rather
// than by habit.
func (p Purpose) Directs() bool { return directs[p] }

// directs : Which purposes let the model choose. One of them.
//
// A purpose added here is a purpose that may call tools in a loop,
// cost several rounds, and end somewhere nobody wrote down. Nothing
// stops that being right -- an open-ended ask needs it -- but it is a
// decision, and TestEveryPurposeSaysWhetherItDirects makes it one.
var directs = map[Purpose]bool{
	PurposeChat: true,

	// Every one of these is a fixed path. Each is one call, or a short
	// sequence the server orders, and none may grow a loop without
	// this line being changed first.
	PurposeTitle:      false,
	PurposeCondense:   false,
	PurposeProfile:    false,
	PurposeGreeting:   false,
	PurposeVocabulary: false,
	PurposeRemark:     false,
}

// Purposes : Every purpose there is, so nothing can be added without
// saying which side of the line it falls on.
func Purposes() []Purpose {
	return []Purpose{
		PurposeChat, PurposeTitle, PurposeCondense, PurposeProfile,
		PurposeGreeting, PurposeVocabulary, PurposeRemark,
	}
}
