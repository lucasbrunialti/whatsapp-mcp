package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEventBrokerFansOutToEverySubscriber(t *testing.T) {
	broker := NewEventBroker()

	firstID, first := broker.Subscribe()
	secondID, second := broker.Subscribe()
	defer broker.Unsubscribe(firstID)
	defer broker.Unsubscribe(secondID)

	if got := broker.SubscriberCount(); got != 2 {
		t.Fatalf("SubscriberCount() = %d, want 2", got)
	}

	broker.Publish(MessageEvent{ID: "msg-1", Content: "oi"})

	for name, stream := range map[string]<-chan MessageEvent{"first": first, "second": second} {
		select {
		case evt := <-stream:
			if evt.ID != "msg-1" {
				t.Errorf("%s subscriber got ID %q, want %q", name, evt.ID, "msg-1")
			}
		case <-time.After(time.Second):
			t.Errorf("%s subscriber received nothing", name)
		}
	}
}

func TestEventBrokerUnsubscribeStopsDelivery(t *testing.T) {
	broker := NewEventBroker()

	id, stream := broker.Subscribe()
	broker.Unsubscribe(id)

	if got := broker.SubscriberCount(); got != 0 {
		t.Fatalf("SubscriberCount() = %d, want 0", got)
	}

	// Unsubscribing twice must not panic on an already-closed channel.
	broker.Unsubscribe(id)

	// Publishing to nobody must not panic either.
	broker.Publish(MessageEvent{ID: "msg-1"})

	if _, open := <-stream; open {
		t.Fatal("stream should be closed after Unsubscribe")
	}
}

func TestEventBrokerDropsEventsForStalledSubscriber(t *testing.T) {
	broker := NewEventBroker()

	id, stream := broker.Subscribe()
	defer broker.Unsubscribe(id)

	// One more than the buffer holds: the publisher must not block on the
	// overflow, because that would stall message ingestion.
	done := make(chan struct{})
	go func() {
		for i := 0; i < eventBufferSize+1; i++ {
			broker.Publish(MessageEvent{ID: "msg"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a full subscriber buffer")
	}

	if got := len(stream); got != eventBufferSize {
		t.Errorf("buffered %d events, want %d", got, eventBufferSize)
	}
}

// readEvent reads one SSE block, skipping comment lines, and returns the
// decoded payload of its data field.
func readEvent(t *testing.T, reader *bufio.Reader) MessageEvent {
	t.Helper()

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("reading stream: %v", err)
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		var evt MessageEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(strings.TrimSpace(line), "data: ")), &evt); err != nil {
			t.Fatalf("decoding event: %v", err)
		}
		return evt
	}
}

// openStream starts the handler over HTTP and waits until the subscription is
// registered, so a Publish that follows cannot race ahead of the subscriber.
func openStream(t *testing.T, query string) (*EventBroker, *bufio.Reader, func()) {
	t.Helper()

	broker := NewEventBroker()
	server := httptest.NewServer(newEventStreamHandler(broker))

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+query, nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("opening stream: %v", err)
	}

	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want %q", got, "text/event-stream")
	}

	deadline := time.Now().Add(2 * time.Second)
	for broker.SubscriberCount() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("handler never subscribed to the broker")
		}
		time.Sleep(5 * time.Millisecond)
	}

	return broker, bufio.NewReader(resp.Body), func() {
		cancel()
		resp.Body.Close()
		server.Close()
	}
}

func TestEventStreamWithholdsOwnMessagesByDefault(t *testing.T) {
	broker, reader, cleanup := openStream(t, "")
	defer cleanup()

	broker.Publish(MessageEvent{ID: "mine", IsFromMe: true})
	broker.Publish(MessageEvent{ID: "theirs", IsFromMe: false})

	if evt := readEvent(t, reader); evt.ID != "theirs" {
		t.Errorf("first delivered event = %q, want %q", evt.ID, "theirs")
	}
}

func TestEventStreamIncludesOwnMessagesWhenAsked(t *testing.T) {
	broker, reader, cleanup := openStream(t, "?include_from_me=true")
	defer cleanup()

	broker.Publish(MessageEvent{ID: "mine", IsFromMe: true})

	if evt := readEvent(t, reader); evt.ID != "mine" {
		t.Errorf("first delivered event = %q, want %q", evt.ID, "mine")
	}
}

func TestEventStreamDeliversFullPayload(t *testing.T) {
	broker, reader, cleanup := openStream(t, "")
	defer cleanup()

	sent := MessageEvent{
		ID:        "3EB01234",
		ChatJID:   "5511999999999@s.whatsapp.net",
		ChatName:  "Cobli",
		Sender:    "5511999999999",
		Content:   "chegou",
		Timestamp: time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC),
		MediaType: "image",
		Filename:  "foto.jpg",
	}
	broker.Publish(sent)

	got := readEvent(t, reader)
	if got != sent {
		t.Errorf("received %+v, want %+v", got, sent)
	}
}

func TestEventStreamUnsubscribesOnDisconnect(t *testing.T) {
	broker, _, cleanup := openStream(t, "")
	cleanup()

	deadline := time.Now().Add(2 * time.Second)
	for broker.SubscriberCount() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("subscriber was not released after the client disconnected")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestEventStreamRejectsBadRequests(t *testing.T) {
	handler := newEventStreamHandler(NewEventBroker())

	cases := []struct {
		name   string
		method string
		target string
		want   int
	}{
		{"post is not allowed", http.MethodPost, "/api/events", http.StatusMethodNotAllowed},
		{"include_from_me must parse", http.MethodGet, "/api/events?include_from_me=sim", http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler(recorder, httptest.NewRequest(tc.method, tc.target, nil))

			if recorder.Code != tc.want {
				t.Errorf("status = %d, want %d", recorder.Code, tc.want)
			}
		})
	}
}
