// Package mail lets the assistant read the person's email.
//
// Four tools, and none of them writes. What the model can do here is
// look: what has come in, what matches a search, what one message
// actually says, and how much is unread.
//
// Not because writing is impossible to do safely -- gmail.send sends
// without being able to read or delete -- but because nothing has
// asked for it yet, and an assistant that can send mail on a
// misheard sentence is a different proposition from one that can
// read it.
//
// The listings are deliberately thin -- who, what, when, and Gmail's
// own one-line extract. A body is fetched one message at a time, once
// the model has decided which one matters. Ten full messages to answer
// "anything new" would cost more than the answer is worth and arrive
// after the person has stopped waiting.
package mail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/mail"
	"github.com/DhanushRamesh/personal-assistant/internal/prompt"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

// Mailbox : The part of the mail client these tools need.
type Mailbox interface {
	Recent(ctx context.Context, userID string, most int) ([]mail.Message, error)
	Unread(ctx context.Context, userID string, most int) ([]mail.Message, error)
	Search(ctx context.Context, userID, query string, most int) ([]mail.Message, error)
	One(ctx context.Context, userID, id string) (*mail.Message, error)
	Count(ctx context.Context, userID string) (int, error)
}

// Clock : Where now comes from, so "today" in a search means today.
type Clock struct {
	Now      func() time.Time
	Location *time.Location
}

// All : Every mail tool.
func All(box Mailbox, clock Clock) []tool.Tool {
	return []tool.Tool{
		inbox(box),
		search(box),
		read(box),
		unread(box),
	}
}

// inbox : What has arrived.
func inbox(box Mailbox) tool.Tool {
	return tool.Tool{
		Name:   "mail_inbox",
		Domain: "mail",
		Lists:  true,
		Purpose: prompt.Text(
			"Read the newest messages in the inbox.",
			"Gives who each is from, what it is about, when it came and a line of it -- not the whole message.",
		),
		UseWhen: prompt.Block(
			prompt.Text(
				"They ask what has come in, whether there is anything new, or what is in their email.",
				"Read this before answering anything about their mail, rather than saying you cannot see it.",
			),
			prompt.Text(
				"The identifiers this returns are what mail_read needs.",
				"A message cannot be read without first appearing in a listing, so this comes first whenever the whole of one is wanted.",
			),
		),
		Avoid: prompt.Text(
			"Do not call it to find one particular message or sender -- mail_search takes a query and will get there in one call.",
			"Do not call it for a count of unread: mail_unread answers that without reading anything.",
		),
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Properties: map[string]tool.Property{
				"most": {
					Type:        "integer",
					Description: "How many to read, newest first. Ten at most, and fewer is better when it is being read aloud.",
					Minimum:     ptr(1),
					Maximum:     ptr(mail.Most),
					Default:     5,
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "any new emails", Args: `{"most":5,"saying":"looking at your inbox"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				Most int `json:"most"`
			}
			_ = json.Unmarshal(in.Args, &args)

			return listing(ctx, box, in, func(userID string) ([]mail.Message, error) {
				return box.Recent(ctx, userID, args.Most)
			}, "in the inbox")
		},
	}
}

// search : Finding one, by whatever the person remembers about it.
func search(box Mailbox) tool.Tool {
	return tool.Tool{
		Name:   "mail_search",
		Domain: "mail",
		Lists:  true,
		Purpose: prompt.Text(
			"Find messages from somebody, or about something, newest first.",
			"Give a sender in from, or words in about, or both.",
		),
		UseWhen: prompt.Block(
			prompt.Text(
				"They ask about mail from somebody, about something, or from some time.",
				"Almost always a sender: \"anything from Amazon\" means from=\"amazon\", and that is the whole call.",
			),
			prompt.Text(
				"Fill it in from what they said and search.",
				"Do not ask them what to search for -- if they named a sender or a subject, that is the search, and asking them to repeat it as a query is asking them to do this tool's job.",
				"Do not ask them for Gmail syntax. They will not know it and they should not have to.",
			),
			prompt.Text(
				"A name reached you through speech and may be misspelt.",
				"Search the part you are confident of rather than the whole of it -- from=\"amazon\" finds amazon.in, Amazon Pay and amazon-orders, where the full address might find none of them.",
				"Say what you searched for, so a wrong guess is visible rather than silent.",
			),
		),
		Avoid: prompt.Text(
			"Do not use it to read a message you have already found: mail_read takes the identifier and gives the whole of it.",
			"Do not put Gmail syntax in from or about -- they are a name and some words, and the syntax is built here.",
		),
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Properties: map[string]tool.Property{
				"from": {
					Type: "string",
					Description: "Who it is from: a name or part of an address, as they said it. " +
						"\"amazon\" rather than \"amazon.in\" when unsure, since less of it matches more.",
				},
				"about": {
					Type:        "string",
					Description: "Words to look for in the subject or the message, when they described what it was about.",
				},
				"unread": {
					Type:        "boolean",
					Description: "Only unread ones. Set when they asked about new or unread mail specifically.",
				},
				"within_days": {
					Type:        "integer",
					Description: "Only messages newer than this many days, when they said a period such as this week.",
					Minimum:     ptr(1),
					Maximum:     ptr(3650),
				},
				"query": {
					Type: "string",
					Description: "Raw Gmail search, for anything the fields above cannot say -- " +
						"has:attachment, label:receipts, larger:5M. Combined with them when both are given.",
				},
				"most": {
					Type:        "integer",
					Description: "How many to return, newest first. Ten at most.",
					Minimum:     ptr(1),
					Maximum:     ptr(mail.Most),
					Default:     5,
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "did I get any mail from amazon.in",
				Args: `{"from":"amazon","saying":"looking for mail from Amazon"}`},
			{Ask: "anything from Alekhya this week",
				Args: `{"from":"alekhya","within_days":7,"saying":"searching your mail"}`},
			{Ask: "any unread mail about the invoice",
				Args: `{"about":"invoice","unread":true,"saying":"looking for that invoice"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				From       string `json:"from"`
				About      string `json:"about"`
				Unread     bool   `json:"unread"`
				WithinDays int    `json:"within_days"`
				Query      string `json:"query"`
				Most       int    `json:"most"`
			}
			if err := json.Unmarshal(in.Args, &args); err != nil {
				return tool.Failed("the search could not be read: " + err.Error())
			}

			query := gmailQuery(args.From, args.About, args.Query, args.Unread, args.WithinDays)
			if query == "" {
				return tool.Failed(prompt.Text(
					"Nothing was given to search on.",
					"Put the sender in from, or what it was about in about.",
					"Do not ask them to supply a search: whatever they said about the message is the search.",
				))
			}

			return listing(ctx, box, in, func(userID string) ([]mail.Message, error) {
				return box.Search(ctx, userID, query, args.Most)
			}, "matching "+query)
		},
	}
}

// gmailQuery : Gmail's search syntax, built from what was described.
//
// Built here rather than asked for, because the fields are what the
// person said and the syntax is not. Asked for a query, the model
// either invents one or -- measured on 30 September 2026 -- tells the
// person the query it would have used and asks them to supply it.
//
// No in: qualifier is added, and that is the right default rather
// than an omission. Gmail's own default searches all mail except spam
// and trash -- archive included -- which is what "did I get mail from
// Amazon" means. in:inbox would miss anything filed away, and
// in:anywhere would return spam.
func gmailQuery(from, about, raw string, unread bool, withinDays int) string {
	var parts []string
	if from = strings.TrimSpace(from); from != "" {
		parts = append(parts, "from:"+quoted(from))
	}
	if about = strings.TrimSpace(about); about != "" {
		parts = append(parts, quoted(about))
	}
	if unread {
		parts = append(parts, "is:unread")
	}
	if withinDays > 0 {
		parts = append(parts, fmt.Sprintf("newer_than:%dd", withinDays))
	}
	if raw = strings.TrimSpace(raw); raw != "" {
		parts = append(parts, raw)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

// quoted : A term Gmail will read as one term.
//
// Only when it has to be. Gmail treats a space as "and", so a name of
// two words unquoted becomes two separate conditions and matches far
// less than it should.
func quoted(term string) string {
	if !strings.ContainsAny(term, " \t") {
		return term
	}
	return strconv.Quote(term)
}

// read : The whole of one message.
func read(box Mailbox) tool.Tool {
	return tool.Tool{
		Name:   "mail_read",
		Domain: "mail",
		Purpose: prompt.Text(
			"Read one whole message, body included.",
			"Needs the identifier from a listing, so mail_inbox or mail_search comes first.",
		),
		UseWhen: prompt.Text(
			"A line of a message is not enough to answer with, and the whole of it is needed.",
			"One at a time: a body is long, and reading several fills the answer with text nobody asked for.",
		),
		Avoid: prompt.Text(
			"Do not guess an identifier. It comes from a listing and nowhere else, and an invented one fails.",
			"Do not read every message in a listing to summarise them -- the lines in the listing are what summaries are made from.",
		),
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Properties: map[string]tool.Property{
				"id": {
					Type:        "string",
					Description: "The identifier of the message, exactly as a listing gave it.",
				},
			},
			Required: []string{"id"},
		},
		Examples: []tool.Example{
			{Ask: "read me the one from the bank", Args: `{"id":"18f2c...","saying":"reading it now"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(in.Args, &args); err != nil {
				return tool.Failed("the identifier could not be read: " + err.Error())
			}
			id := strings.TrimSpace(args.ID)
			if id == "" {
				return tool.Failed("No message was named, so there is nothing to read.")
			}
			if box == nil {
				return tool.Failed("There is no mailbox on this server.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}

			got, err := box.One(ctx, in.Caller.UserID, id)
			if err != nil {
				return trouble(err)
			}

			var b strings.Builder
			fmt.Fprintf(&b, "From %s", orElse(got.From, "somebody unnamed"))
			if got.Subject != "" {
				fmt.Fprintf(&b, ", about %q", got.Subject)
			}
			if !got.At.IsZero() {
				fmt.Fprintf(&b, ", on %s", got.At.Format("Monday 2 January at 15:04"))
			}
			b.WriteString(".\n\n")
			b.WriteString(orElse(got.Body, "It has no readable text in it."))
			return tool.OK(b.String())
		},
	}
}

// unread : How much is waiting, without reading any of it.
func unread(box Mailbox) tool.Tool {
	return tool.Tool{
		Name:   "mail_unread",
		Domain: "mail",
		Purpose: prompt.Text(
			"Say how many unread messages are in the inbox, and name the newest few.",
		),
		UseWhen: prompt.Text(
			"They ask how much mail is waiting, or whether anything is unread.",
			"This gives the true count, which a listing cannot: a listing stops at ten and the answer to \"how many\" is then always ten.",
		),
		Avoid: prompt.Text(
			"Do not use it to read anything. It counts, and names the newest few; mail_read reads.",
		),
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params:   tool.Schema{Properties: map[string]tool.Property{}},
		Examples: []tool.Example{
			{Ask: "how many unread emails do I have", Args: `{"saying":"counting your unread mail"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			if box == nil {
				return tool.Failed("There is no mailbox on this server.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}

			count, err := box.Count(ctx, in.Caller.UserID)
			if err != nil {
				return trouble(err)
			}
			if count == 0 {
				return tool.OK("Nothing is unread in the inbox.")
			}

			var b strings.Builder
			fmt.Fprintf(&b, "%d unread %s in the inbox.", count, plural("message", count))

			// The newest few alongside the number, because "you have
			// forty-one unread" on its own invites the same person to
			// ask "from who" and spend another round on it.
			newest, err := box.Unread(ctx, in.Caller.UserID, 3)
			if err == nil && len(newest) > 0 {
				b.WriteString(" The newest:")
				for _, m := range newest {
					b.WriteString("\n" + line(m))
				}
			}
			return tool.OK(b.String())
		},
	}
}

// listing : The shared shape of the two tools that return several.
func listing(
	ctx context.Context,
	box Mailbox,
	in tool.Invocation,
	fetch func(userID string) ([]mail.Message, error),
	where string,
) tool.Result {
	if box == nil {
		return tool.Failed("There is no mailbox on this server.")
	}
	if in.Caller.UserID == "" {
		return tool.Failed("This request did not come from a known person.")
	}

	found, err := fetch(in.Caller.UserID)
	if err != nil {
		return trouble(err)
	}
	if len(found) == 0 {
		return tool.OK("Nothing " + where + ".")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%d %s %s, newest first:", len(found), plural("message", len(found)), where)
	for _, m := range found {
		b.WriteString("\n" + line(m))
	}
	return tool.OK(b.String())
}

// line : One message on one line, with the identifier reading can use.
func line(m mail.Message) string {
	var b strings.Builder
	b.WriteString("- ")
	if m.Unread {
		b.WriteString("unread, ")
	}
	fmt.Fprintf(&b, "from %s", orElse(m.From, "somebody unnamed"))
	if m.Subject != "" {
		fmt.Fprintf(&b, ", %q", m.Subject)
	}
	if !m.At.IsZero() {
		fmt.Fprintf(&b, ", %s", m.At.Format("Mon 2 Jan 15:04"))
	}
	if m.Snippet != "" {
		fmt.Fprintf(&b, " -- %s", m.Snippet)
	}
	fmt.Fprintf(&b, " (id %s)", m.ID)
	return b.String()
}

// trouble : What to say when Google would not answer.
//
// The reason is passed on rather than flattened. A refused scope and a
// network failure need different things done about them, and "could
// not read your mail" leaves the model to invent which.
func trouble(err error) tool.Result {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return tool.Failed("Reading the mail took too long and was given up on.")
	}
	return tool.Failed("Google would not answer: " + err.Error())
}

// orElse : A string, or a stand-in when it is empty.
func orElse(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// plural : The word, made plural when it needs to be.
func plural(word string, n int) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// ptr : An addressable copy, for the schema's bounds.
func ptr(n int) *int { return &n }
