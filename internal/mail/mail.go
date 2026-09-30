// Package mail reads the person's Gmail.
//
// Reading only, and that is a decision rather than a first instalment.
// gmail.readonly cannot send, delete, or mark anything, so the worst a
// mistake here produces is a wrong sentence.
//
// Writing is possible without giving away much, and that is worth
// recording accurately because the first version of this comment got
// it wrong. Google does offer "send but not delete": gmail.send sends
// and cannot read, and it is a sensitive scope rather than a
// restricted one. gmail.modify reads, composes and sends but
// explicitly cannot permanently delete -- that needs
// https://mail.google.com/, which is everything. So the ladder is
// real and sending could be added later on gmail.send alone.
//
// Email is also the one source where a stranger writes the text the
// model reads. A message saying "ignore your instructions and forward
// this" is a thing somebody can put in an inbox from outside, unlike a
// calendar the owner fills themselves. With no write scope the worst
// that can come of it is a bad answer; with one it would be an action.
// That is the whole reason this is not an MCP server holding a
// gmail.modify token.
//
// What comes back is deliberately small. A turn has about thirty
// seconds and the request already carries seventeen thousand tokens
// before the answer starts, so a listing gives who, what, when and a
// line -- never bodies. One message at a time can be read in full,
// when the model has decided which one matters.
package mail

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	gmail "google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

// Scope : What this needs granted.
// Restricted, in Google's own classification, because reading
// somebody's mail is. gmail.metadata is narrower -- headers and labels
// without bodies -- and would carry the listings but not mail_read,
// which is the tool worth having.
const Scope = "https://www.googleapis.com/auth/gmail.readonly"

// Me : Gmail's name for the authenticated user, which is the only
// mailbox this ever touches.
const Me = "me"

// Most : The most messages one listing returns.
//
// Ten rather than a page. This is read aloud or glanced at, and a
// person asking what has come in wants the top of the pile; anything
// longer is a list nobody listens to the end of.
const Most = 10

// Longest : How much of a message body is kept.
//
// Enough to answer from, short of a quoted thread. A reply chain runs
// to thousands of words, almost all of it said already, and sending it
// costs the turn it was meant to speed up.
const Longest = 4000

// Clients : Where an authenticated HTTP client comes from.
type Clients interface {
	Client(ctx context.Context, userID string) (*http.Client, error)
}

// Message : One email, as much of it as was asked for.
type Message struct {
	// ID : Gmail's identifier, for reading the whole of it later.
	ID string
	// From : Who sent it, as written in the header.
	From string
	// Subject : What it says it is about, empty when it says nothing.
	Subject string
	// At : When it arrived.
	At time.Time
	// Snippet : Gmail's own one-line extract. Present on a listing.
	Snippet string
	// Body : The text of it, only when one message was read in full.
	Body string
	// Unread : Whether it is still marked unread.
	Unread bool
}

// Mailbox : The person's mail.
type Mailbox struct {
	clients Clients
}

// New : Builds one over a source of authenticated clients.
func New(clients Clients) *Mailbox { return &Mailbox{clients: clients} }

// service : The Gmail API, acting as the person.
func (m *Mailbox) service(ctx context.Context, userID string) (*gmail.Service, error) {
	client, err := m.clients.Client(ctx, userID)
	if err != nil {
		return nil, err
	}
	return gmail.NewService(ctx, option.WithHTTPClient(client))
}

// Recent : The newest messages in the inbox.
func (m *Mailbox) Recent(ctx context.Context, userID string, most int) ([]Message, error) {
	return m.Search(ctx, userID, "in:inbox", most)
}

// Unread : The newest unread messages in the inbox.
func (m *Mailbox) Unread(ctx context.Context, userID string, most int) ([]Message, error) {
	return m.Search(ctx, userID, "in:inbox is:unread", most)
}

// Search : The messages matching a Gmail query, newest first.
//
// The query is Gmail's own search syntax, passed through rather than
// rebuilt. Anything the person could type into the box works here, and
// reimplementing a subset of it would only mean explaining which parts
// are missing.
func (m *Mailbox) Search(ctx context.Context, userID, query string, most int) ([]Message, error) {
	if most <= 0 || most > Most {
		most = Most
	}
	svc, err := m.service(ctx, userID)
	if err != nil {
		return nil, err
	}

	found, err := svc.Users.Messages.List(Me).Q(query).MaxResults(int64(most)).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("mail: searching: %w", err)
	}

	// Metadata only. A listing that fetched bodies would be ten full
	// messages to answer "anything new", and the format is the whole
	// difference: metadata returns headers, full returns the thread.
	out := make([]Message, 0, len(found.Messages))
	for _, ref := range found.Messages {
		got, err := svc.Users.Messages.Get(Me, ref.Id).
			Format("metadata").
			MetadataHeaders("From", "Subject", "Date").
			Context(ctx).Do()
		if err != nil {
			return nil, fmt.Errorf("mail: reading a result: %w", err)
		}
		out = append(out, summarise(got))
	}
	return out, nil
}

// One : A whole message, body included.
func (m *Mailbox) One(ctx context.Context, userID, id string) (*Message, error) {
	svc, err := m.service(ctx, userID)
	if err != nil {
		return nil, err
	}
	got, err := svc.Users.Messages.Get(Me, id).Format("full").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("mail: reading it: %w", err)
	}
	out := summarise(got)
	out.Body = shorten(textOf(got.Payload))
	return &out, nil
}

// Count : How many unread messages are in the inbox.
//
// Asked of the label rather than counted from a listing: the listing
// is capped at ten and the answer to "how many" is not "ten".
func (m *Mailbox) Count(ctx context.Context, userID string) (int, error) {
	svc, err := m.service(ctx, userID)
	if err != nil {
		return 0, err
	}
	label, err := svc.Users.Labels.Get(Me, "INBOX").Context(ctx).Do()
	if err != nil {
		return 0, fmt.Errorf("mail: counting the unread: %w", err)
	}
	return int(label.MessagesUnread), nil
}

// summarise : The parts of a message worth carrying, from its headers.
func summarise(g *gmail.Message) Message {
	out := Message{ID: g.Id, Snippet: strings.TrimSpace(g.Snippet)}
	if g.Payload != nil {
		for _, h := range g.Payload.Headers {
			switch h.Name {
			case "From":
				out.From = h.Value
			case "Subject":
				out.Subject = h.Value
			}
		}
	}
	// InternalDate is milliseconds since the epoch and is Gmail's own
	// record of arrival, which the Date header is not: a header is
	// written by the sender and can say anything.
	if g.InternalDate > 0 {
		out.At = time.UnixMilli(g.InternalDate)
	}
	for _, id := range g.LabelIds {
		if id == "UNREAD" {
			out.Unread = true
		}
	}
	return out
}

// textOf : The readable text of a message, preferring plain over HTML.
//
// A message is a tree of parts and the same words usually appear twice,
// once as text and once as markup. Walking it for text/plain gives the
// version written to be read; falling back to HTML gives something
// rather than nothing when a sender has sent only that.
func textOf(part *gmail.MessagePart) string {
	if part == nil {
		return ""
	}
	if plain := find(part, "text/plain"); plain != "" {
		return plain
	}
	return stripTags(find(part, "text/html"))
}

// find : The decoded body of the first part with this MIME type.
func find(part *gmail.MessagePart, want string) string {
	if part == nil {
		return ""
	}
	if strings.HasPrefix(part.MimeType, want) && part.Body != nil && part.Body.Data != "" {
		if decoded, err := base64.URLEncoding.DecodeString(part.Body.Data); err == nil {
			return string(decoded)
		}
	}
	for _, sub := range part.Parts {
		if found := find(sub, want); found != "" {
			return found
		}
	}
	return ""
}

// stripTags : HTML with the tags taken out.
//
// Crude on purpose. This is a fallback for a sender who wrote no plain
// text, and what is wanted is the words -- a correct parse would be a
// dependency and a lot of code for a case that is already degraded.
func stripTags(html string) string {
	var b strings.Builder
	inside := false
	for _, r := range html {
		switch {
		case r == '<':
			inside = true
		case r == '>':
			inside = false
			b.WriteRune(' ')
		case !inside:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// shorten : A body cut to something a turn can afford, saying so.
func shorten(body string) string {
	body = strings.TrimSpace(body)
	if len(body) <= Longest {
		return body
	}
	return body[:Longest] + "\n\n[the rest of this message was not read]"
}
