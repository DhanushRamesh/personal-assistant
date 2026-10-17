package environment

import (
	"cmp"
	"context"
	"time"
)

// Feature : Which part of the server asked a model something.
//
// Not Purpose. A purpose is why the request is made and changes how it
// is made -- how hard the model may think, which model -- and several
// callers share one: the rule writer, the noticing pass and a rule's
// own remark are all PurposeRemark. A feature is only who asked, so
// that what each of them costs can be told apart afterwards.
type Feature string

// The features that ask a model anything. Each caller of Run names one
// on the context with Labelled; a call that reaches the model without
// one is kept as FeatureOther, and said so in the log.
const (
	// FeatureChat : A round of answering the person, tool rounds
	// included.
	FeatureChat Feature = "chat"
	// FeatureTitle : Naming a conversation.
	FeatureTitle Feature = "title"
	// FeatureCondense : Condensing the earlier part of a conversation.
	FeatureCondense Feature = "condense"
	// FeatureProfile : Rewriting the standing description of the person.
	FeatureProfile Feature = "profile"
	// FeatureVocabulary : Collecting the names they say.
	FeatureVocabulary Feature = "vocabulary"
	// FeatureGreeting : What is said at the door.
	FeatureGreeting Feature = "greeting"
	// FeatureRuleWriter : Turning a rule asked for into one, from the
	// rule tools.
	FeatureRuleWriter Feature = "rule_writer"
	// FeatureComing : Reading what is coming in the diary.
	FeatureComing Feature = "coming"
	// FeatureNoticingWriter : The rule writer, writing a rule the
	// noticing pass decided on.
	FeatureNoticingWriter Feature = "noticing_writer"
	// FeatureNoticing : The pass that judges whether anything is worth a
	// rule, a retirement or a superseded memory.
	FeatureNoticing Feature = "noticing"
	// FeatureRuleRemark : A rule firing, writing what it says.
	FeatureRuleRemark Feature = "rule_remark"
	// FeatureTerminal : A terminal on the laptop wanting attention,
	// deciding whether to tell them and what.
	FeatureTerminal Feature = "terminal"
	// FeatureOther : A call nobody labelled.
	FeatureOther Feature = "other"
)

// Features : Every feature, in the order a reader would list them.
func Features() []Feature {
	return []Feature{
		FeatureChat, FeatureTitle, FeatureCondense, FeatureProfile,
		FeatureVocabulary, FeatureGreeting, FeatureRuleWriter, FeatureComing,
		FeatureNoticing, FeatureNoticingWriter, FeatureRuleRemark, FeatureTerminal,
		FeatureOther,
	}
}

// Detail values a caller or an environment may put on a label.
const (
	// DetailTools : The round asked for tools rather than answering.
	DetailTools = "tools"
	// DetailEarly : A chat round started beside the one in front of it,
	// in case that one is sent back. See the runner's ahead.
	DetailEarly = "started early"
)

// Label : Who a call to a model is for, and what asked it.
//
// Carried on the context rather than on Request, because the person
// and the chat are known several calls above the one that reaches the
// model -- a tool run inside a chat asks a model of its own, and knows
// nothing of whose chat it is.
type Label struct {
	// UserID : Whose call it is. Empty only where nothing above knew.
	UserID string
	// Feature : What asked.
	Feature Feature
	// Detail : Anything narrower worth keeping beside the feature -- a
	// rule's name, DetailEarly.
	Detail string
	// ChatID, ConversationID : The chat being answered, when there is one.
	ChatID         string
	ConversationID string
	// Early : Whether this is a round started ahead of need, which the
	// caller may drop unread. Such a round ending unread was not
	// stopped by anybody; it was not used.
	Early bool
}

// labelKey : Where a Label is kept on a context.
type labelKey struct{}

// Labelled : ctx, carrying l over whatever label it already had.
//
// The ids are kept from the label above when l leaves them empty, which
// is what lets a tool run inside a chat say only what it is. A feature
// brings its own detail and its own Early: the chat's "tools" is not
// the rule writer's.
func Labelled(ctx context.Context, l Label) context.Context {
	was, _ := ctx.Value(labelKey{}).(Label)
	if l.UserID == "" {
		l.UserID = was.UserID
	}
	if l.ChatID == "" {
		l.ChatID = was.ChatID
	}
	if l.ConversationID == "" {
		l.ConversationID = was.ConversationID
	}
	if l.Feature == "" {
		l.Feature = was.Feature
		l.Detail = cmp.Or(l.Detail, was.Detail)
		l.Early = l.Early || was.Early
	}
	return context.WithValue(ctx, labelKey{}, l)
}

// LabelOf : The label ctx carries, and whether it carries one.
func LabelOf(ctx context.Context) (Label, bool) {
	l, ok := ctx.Value(labelKey{}).(Label)
	return l, ok
}

// Ended : How a call to a model came out.
type Ended string

const (
	// EndedAnswered : The model answered in words.
	EndedAnswered Ended = "answered"
	// EndedTools : The model asked for tools to be run.
	EndedTools Ended = "tools"
	// EndedFailed : The call failed, or was refused.
	EndedFailed Ended = "failed"
	// EndedStopped : The caller stopped listening before it ended.
	EndedStopped Ended = "stopped"
	// EndedUnused : A round started early that the caller dropped.
	EndedUnused Ended = "unused"
)

// Call : One request sent to a model, as it is kept.
//
// One per attempt: a call retried after a dead connection is two calls,
// because both were sent and both may have been charged.
type Call struct {
	Label
	// Vendor, Model : What was actually sent, after defaults.
	Vendor string
	Model  string
	// Input, Output : The tokens the service said it charged, zero where
	// it said nothing.
	Input  int
	Output int
	// At : When it was sent.
	At time.Time
	// Took : From sending to the end of the reply.
	Took time.Duration
	// Ended : How it came out.
	Ended Ended
}

// Recorder : Somewhere each call to a model is kept.
//
// Record is not allowed to fail the call: by the time it is called the
// tokens are spent, and an answer is not to be lost over its receipt.
type Recorder interface {
	Record(ctx context.Context, c Call)
}
