// Package logtest provides an in-memory sink for asserting on log output in
// tests, so a test does not have to wire up a buffer and parse text back out.
//
//	observer, logger := logtest.New(log.DEBUG)
//	// exercise the code under test
//	if got := observer.FilterMessage("cache miss").Len(); got != 1 {
//	    t.Fatalf("expected one cache miss, got %d", got)
//	}
package logtest

import (
	"strings"
	"sync"
	"testing"

	log "github.com/pod32g/simple-logger"
)

// Entry is one recorded log entry.
type Entry struct {
	Level   log.LogLevel
	Message string
	Fields  []log.Field
}

// Field returns the value recorded under key, and whether it was present.
// Groups are addressed with a dotted path ("http.status"); a flat key that
// itself contains dots is matched exactly first.
func (e Entry) Field(key string) (interface{}, bool) {
	return lookupField(e.Fields, key)
}

func lookupField(fields []log.Field, key string) (interface{}, bool) {
	// An exact match wins over path traversal: flat keys containing dots are a
	// convention of their own ("grpc.method", "http.status_code"), and a test
	// asking for one should not have to know whether it was a group.
	for _, f := range fields {
		if f.Key == key {
			return f.Value, true
		}
	}

	head, rest, nested := strings.Cut(key, ".")
	for _, f := range fields {
		if f.Key != head {
			continue
		}
		if !nested {
			return f.Value, true
		}
		if group, ok := f.Value.([]log.Field); ok {
			return lookupField(group, rest)
		}
		return nil, false
	}
	return nil, false
}

// Observer records entries as they are emitted.
type Observer struct {
	mu      sync.Mutex
	entries []Entry
}

// New returns an Observer and a Logger that reports to it. The logger writes
// nowhere else, so tests stay quiet.
func New(level log.LogLevel) (*Observer, *log.Logger) {
	obs := &Observer{}
	logger := log.Must(log.New(
		log.WithOutput(nil), // discard
		log.WithLevel(level),
		log.WithHook(obs),
	))
	return obs, logger
}

// Attach records everything an existing logger emits, leaving its output alone.
func Attach(logger *log.Logger) *Observer {
	obs := &Observer{}
	logger.AddHook(obs)
	return obs
}

// Fire implements log.Hook. Hooks see entries after redaction and field
// normalisation, which is what a test wants to assert on.
func (o *Observer) Fire(level log.LogLevel, message string, fields []log.Field) {
	cloned := make([]log.Field, len(fields))
	copy(cloned, fields)

	o.mu.Lock()
	defer o.mu.Unlock()
	o.entries = append(o.entries, Entry{Level: level, Message: message, Fields: cloned})
}

// All returns every entry recorded so far.
func (o *Observer) All() []Entry {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]Entry, len(o.entries))
	copy(out, o.entries)
	return out
}

// TakeAll returns every entry recorded so far and clears the record.
func (o *Observer) TakeAll() []Entry {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := o.entries
	o.entries = nil
	return out
}

// Len is the number of recorded entries.
func (o *Observer) Len() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.entries)
}

// Filtered is a narrowed view, so filters can be chained.
type Filtered []Entry

// FilterLevel keeps entries at exactly the given level.
func (o *Observer) FilterLevel(level log.LogLevel) Filtered {
	return Filtered(o.All()).FilterLevel(level)
}

// FilterMessage keeps entries whose message is exactly msg.
func (o *Observer) FilterMessage(msg string) Filtered {
	return Filtered(o.All()).FilterMessage(msg)
}

// FilterField keeps entries carrying key with the given value.
func (o *Observer) FilterField(key string, value interface{}) Filtered {
	return Filtered(o.All()).FilterField(key, value)
}

func (f Filtered) FilterLevel(level log.LogLevel) Filtered {
	var out Filtered
	for _, e := range f {
		if e.Level == level {
			out = append(out, e)
		}
	}
	return out
}

func (f Filtered) FilterMessage(msg string) Filtered {
	var out Filtered
	for _, e := range f {
		if e.Message == msg {
			out = append(out, e)
		}
	}
	return out
}

func (f Filtered) FilterField(key string, value interface{}) Filtered {
	var out Filtered
	for _, e := range f {
		if got, ok := e.Field(key); ok && got == value {
			out = append(out, e)
		}
	}
	return out
}

// Len is the number of entries in the view.
func (f Filtered) Len() int { return len(f) }

// Messages lists the messages in the view, which makes a failure message
// readable without a custom formatter.
func (f Filtered) Messages() []string {
	out := make([]string, 0, len(f))
	for _, e := range f {
		out = append(out, e.Message)
	}
	return out
}

// AssertLogged fails the test unless exactly one entry matches message.
func (o *Observer) AssertLogged(t testing.TB, message string) Entry {
	t.Helper()
	matches := o.FilterMessage(message)
	if len(matches) != 1 {
		t.Fatalf("expected exactly one entry %q, found %d in %v", message, len(matches), Filtered(o.All()).Messages())
	}
	return matches[0]
}
