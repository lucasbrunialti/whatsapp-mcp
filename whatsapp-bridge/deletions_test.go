package main

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func newTestMessageStore(t *testing.T) *MessageStore {
	t.Helper()
	db, err := sql.Open("sqlite3", "file::memory:?_foreign_keys=on")
	if err != nil {
		t.Fatal(err)
	}
	// One connection, so every query sees the same in-memory database.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if err := initMessageSchema(db); err != nil {
		t.Fatal(err)
	}
	return &MessageStore{db: db}
}

func storeTestMessage(t *testing.T, store *MessageStore, id, chatJID, sender, content string, timestamp time.Time) {
	t.Helper()
	if err := store.StoreChat(chatJID, "Fornecedor", timestamp); err != nil {
		t.Fatal(err)
	}
	if err := store.StoreMessage(id, chatJID, sender, content, timestamp, false, "", "", "", nil, nil, nil, 0); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateDeletionColumnsUpgradesExistingDatabase(t *testing.T) {
	db, err := sql.Open("sqlite3", "file::memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()

	// The schema before deletions were tracked.
	if _, err := db.Exec(`
		CREATE TABLE chats (jid TEXT PRIMARY KEY, name TEXT, last_message_time TIMESTAMP);
		CREATE TABLE messages (id TEXT, chat_jid TEXT, sender TEXT, content TEXT, timestamp TIMESTAMP,
			is_from_me BOOLEAN, media_type TEXT, filename TEXT, url TEXT, media_key BLOB, file_sha256 BLOB,
			file_enc_sha256 BLOB, file_length INTEGER, PRIMARY KEY (id, chat_jid));
		INSERT INTO messages (id, chat_jid, content) VALUES ('old', 'chat', 'kept');
	`); err != nil {
		t.Fatal(err)
	}

	// Running twice must be a no-op the second time.
	for i := 0; i < 2; i++ {
		if err := initMessageSchema(db); err != nil {
			t.Fatalf("initMessageSchema() run %d: %v", i+1, err)
		}
	}

	var content string
	var deletedAt sql.NullTime
	if err := db.QueryRow("SELECT content, deleted_at FROM messages WHERE id = 'old'").Scan(&content, &deletedAt); err != nil {
		t.Fatal(err)
	}
	if content != "kept" || deletedAt.Valid {
		t.Fatalf("existing row = %q, deleted_at valid %v; want kept and not deleted", content, deletedAt.Valid)
	}
}

func TestMarkMessageDeletedKeepsContent(t *testing.T) {
	store := newTestMessageStore(t)
	sent := time.Date(2030, 1, 15, 9, 0, 0, 0, time.UTC)
	storeTestMessage(t, store, "msg-1", "5511999999999@s.whatsapp.net", "5511999999999", "Preço final: R$ 1.200", sent)

	deletion := MessageDeletion{DeletedAt: sent.Add(time.Hour), DeletedBy: "5511999999999", Scope: DeleteScopeEveryone}
	deleted, err := store.MarkMessageDeleted("msg-1", "5511999999999@s.whatsapp.net", "5511999999999", deletion.DeletedAt, false, deletion)
	if err != nil {
		t.Fatal(err)
	}

	if deleted.Content != "Preço final: R$ 1.200" || !deleted.Timestamp.Equal(sent) {
		t.Fatalf("MarkMessageDeleted() = %+v, want original content and timestamp", deleted)
	}
	got, err := store.GetMessageDeletion("msg-1", "5511999999999@s.whatsapp.net")
	if err != nil || got == nil {
		t.Fatalf("GetMessageDeletion() = %v, %v; want a deletion", got, err)
	}
	if got.Scope != DeleteScopeEveryone || got.DeletedBy != "5511999999999" || !got.DeletedAt.Equal(deletion.DeletedAt) {
		t.Fatalf("GetMessageDeletion() = %+v, want %+v", got, deletion)
	}
}

func TestMarkMessageDeletedCreatesTombstoneForUnknownMessage(t *testing.T) {
	store := newTestMessageStore(t)
	chatJID := "5511999999999@s.whatsapp.net"
	when := time.Date(2030, 1, 15, 10, 0, 0, 0, time.UTC)
	if err := store.EnsureChat(chatJID, "Fornecedor", when); err != nil {
		t.Fatal(err)
	}

	deletion := MessageDeletion{DeletedAt: when, DeletedBy: "5511999999999", Scope: DeleteScopeEveryone}
	deleted, err := store.MarkMessageDeleted("never-seen", chatJID, "5511999999999", when, false, deletion)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Content != "" || deleted.Sender != "5511999999999" {
		t.Fatalf("tombstone = %+v, want empty content from the original sender", deleted)
	}
	if got, _ := store.GetMessageDeletion("never-seen", chatJID); got == nil {
		t.Fatal("tombstone is not marked as deleted")
	}
}

func TestDeleteForMeDoesNotDowngradeDeleteForEveryone(t *testing.T) {
	store := newTestMessageStore(t)
	chatJID := "5511999999999@s.whatsapp.net"
	sent := time.Date(2030, 1, 15, 9, 0, 0, 0, time.UTC)
	storeTestMessage(t, store, "msg-1", chatJID, "5511999999999", "oi", sent)

	everyone := MessageDeletion{DeletedAt: sent.Add(time.Minute), DeletedBy: "5511999999999", Scope: DeleteScopeEveryone}
	me := MessageDeletion{DeletedAt: sent.Add(time.Hour), DeletedBy: "5511888888888", Scope: DeleteScopeMe, ByMe: true}
	if _, err := store.MarkMessageDeleted("msg-1", chatJID, "5511999999999", sent, false, everyone); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkMessageDeleted("msg-1", chatJID, "5511999999999", sent, false, me); err != nil {
		t.Fatal(err)
	}

	got, _ := store.GetMessageDeletion("msg-1", chatJID)
	if got == nil || got.Scope != DeleteScopeEveryone || got.DeletedBy != "5511999999999" {
		t.Fatalf("GetMessageDeletion() = %+v, want the delete for everyone kept", got)
	}
}

func TestStoreMessageKeepsDeletionWhenStoredAgain(t *testing.T) {
	store := newTestMessageStore(t)
	chatJID := "5511999999999@s.whatsapp.net"
	sent := time.Date(2030, 1, 15, 9, 0, 0, 0, time.UTC)
	storeTestMessage(t, store, "msg-1", chatJID, "5511999999999", "oi", sent)

	deletion := MessageDeletion{DeletedAt: sent.Add(time.Minute), DeletedBy: "5511999999999", Scope: DeleteScopeEveryone}
	if _, err := store.MarkMessageDeleted("msg-1", chatJID, "5511999999999", sent, false, deletion); err != nil {
		t.Fatal(err)
	}

	// History sync stores the same message again after re-pairing.
	storeTestMessage(t, store, "msg-1", chatJID, "5511999999999", "oi", sent)

	if got, _ := store.GetMessageDeletion("msg-1", chatJID); got == nil {
		t.Fatal("storing the message again erased its deletion")
	}
}

func TestMarkChatDeletedMarksOnlyThatChat(t *testing.T) {
	store := newTestMessageStore(t)
	sent := time.Date(2030, 1, 15, 9, 0, 0, 0, time.UTC)
	storeTestMessage(t, store, "a-1", "a@s.whatsapp.net", "a", "um", sent)
	storeTestMessage(t, store, "a-2", "a@s.whatsapp.net", "a", "dois", sent.Add(time.Minute))
	storeTestMessage(t, store, "b-1", "b@s.whatsapp.net", "b", "outro chat", sent)

	deletion := MessageDeletion{DeletedAt: sent.Add(time.Hour), DeletedBy: "me", Scope: DeleteScopeChat, ByMe: true}
	count, err := store.MarkChatDeleted("a@s.whatsapp.net", deletion)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("MarkChatDeleted() marked %d messages, want 2", count)
	}
	if got, _ := store.GetMessageDeletion("b-1", "b@s.whatsapp.net"); got != nil {
		t.Fatal("MarkChatDeleted() touched another chat")
	}
}

func TestHandleRevokeMarksOriginalAndPublishesEvent(t *testing.T) {
	store := newTestMessageStore(t)
	broker := NewEventBroker()
	subID, stream := broker.Subscribe()
	defer broker.Unsubscribe(subID)

	group := types.NewJID("120363000000000000", types.GroupServer)
	sent := time.Date(2030, 1, 15, 9, 0, 0, 0, time.UTC)
	storeTestMessage(t, store, "original", group.String(), "5511999999999", "Reunião cancelada", sent)

	// A group admin deletes another participant's message for everyone.
	revoke := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:   group,
				Sender: types.NewJID("5511777777777", types.DefaultUserServer),
			},
			ID:        "revoke",
			Timestamp: sent.Add(time.Hour),
		},
		Message: &waProto.Message{ProtocolMessage: &waProto.ProtocolMessage{
			Type: waProto.ProtocolMessage_REVOKE.Enum(),
			Key: &waProto.MessageKey{
				ID:          proto.String("original"),
				FromMe:      proto.Bool(false),
				Participant: proto.String("5511999999999@s.whatsapp.net"),
			},
		}},
	}

	if !handleRevoke(nil, store, broker, revoke, "Time", &recordingLogger{}) {
		t.Fatal("handleRevoke() = false, want true for a revoke")
	}

	got, _ := store.GetMessageDeletion("original", group.String())
	if got == nil || got.DeletedBy != "5511777777777" || got.Scope != DeleteScopeEveryone {
		t.Fatalf("GetMessageDeletion() = %+v, want deleted for everyone by the admin", got)
	}
	var count int
	store.db.QueryRow("SELECT COUNT(*) FROM messages WHERE id = 'revoke'").Scan(&count)
	if count != 0 {
		t.Fatal("the revoke itself was stored as a message")
	}

	select {
	case evt := <-stream:
		if evt.sseName() != "message_deleted" || evt.Type != MessageEventTypeDeleted {
			t.Fatalf("event %q/%q, want message_deleted/deleted", evt.sseName(), evt.Type)
		}
		if evt.ID != "original" || evt.Content != "Reunião cancelada" || evt.Sender != "5511999999999" {
			t.Fatalf("event = %+v, want the original message", evt)
		}
		payload, _ := json.Marshal(evt)
		var decoded map[string]any
		json.Unmarshal(payload, &decoded)
		deletion, _ := decoded["deletion"].(map[string]any)
		if deletion["deleted_by"] != "5511777777777" || deletion["scope"] != "everyone" || deletion["by_me"] != false {
			t.Fatalf("deletion payload = %v", deletion)
		}
	case <-time.After(time.Second):
		t.Fatal("no deletion event published")
	}
}

func TestHandleRevokeIgnoresOtherMessages(t *testing.T) {
	store := newTestMessageStore(t)
	msg := &events.Message{Message: &waProto.Message{Conversation: proto.String("oi")}}
	if handleRevoke(nil, store, NewEventBroker(), msg, "", &recordingLogger{}) {
		t.Fatal("handleRevoke() = true for a normal message")
	}
	edit := &events.Message{Message: &waProto.Message{ProtocolMessage: &waProto.ProtocolMessage{
		Type: waProto.ProtocolMessage_MESSAGE_EDIT.Enum(),
		Key:  &waProto.MessageKey{ID: proto.String("x")},
	}}}
	if handleRevoke(nil, store, NewEventBroker(), edit, "", &recordingLogger{}) {
		t.Fatal("handleRevoke() = true for an edit")
	}
}

func TestDeletionEventsHonourIncludeFromMe(t *testing.T) {
	theirs := MessageEvent{IsFromMe: true, Deletion: &MessageDeletion{ByMe: false, Scope: DeleteScopeEveryone}}
	if theirs.fromMe() {
		t.Fatal("a deletion by someone else of my message must reach the stream")
	}
	mine := MessageEvent{IsFromMe: false, Deletion: &MessageDeletion{ByMe: true, Scope: DeleteScopeMe}}
	if !mine.fromMe() {
		t.Fatal("a deletion I made is from me")
	}
	if got := (MessageEvent{Deletion: &MessageDeletion{Scope: DeleteScopeChatCleared}}).sseName(); got != "chat_deleted" {
		t.Fatalf("sseName() = %q, want chat_deleted", got)
	}
	if got := (MessageEvent{}).sseName(); got != "message" {
		t.Fatalf("sseName() = %q, want message", got)
	}
}
