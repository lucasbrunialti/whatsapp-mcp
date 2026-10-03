package main

import (
	"database/sql"
	"fmt"
	"time"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// Deleted messages are never removed from the database. They keep their
// original content and are marked with when, by whom and how they were
// deleted, so an agent can reason about it ("the supplier deleted the price
// they sent an hour ago") instead of losing the message.
const (
	// DeleteScopeEveryone is "delete for everyone" (a revoke), by the sender
	// or by a group admin.
	DeleteScopeEveryone = "everyone"
	// DeleteScopeMe is "delete for me", done on another of the user's devices.
	DeleteScopeMe = "me"
	// DeleteScopeChat means the whole chat was deleted on another device.
	DeleteScopeChat = "chat"
	// DeleteScopeChatCleared means the chat was cleared on another device.
	DeleteScopeChatCleared = "chat_cleared"
)

// MessageDeletion describes how a message was deleted.
type MessageDeletion struct {
	DeletedAt time.Time `json:"deleted_at"`
	DeletedBy string    `json:"deleted_by"`
	Scope     string    `json:"scope"`
	ByMe      bool      `json:"by_me"`
}

// deletionColumns are added to existing databases on startup.
var deletionColumns = []struct{ name, definition string }{
	{"deleted_at", "TIMESTAMP"},
	{"deleted_by", "TEXT"},
	{"delete_scope", "TEXT"},
}

func migrateDeletionColumns(db *sql.DB) error {
	rows, err := db.Query("PRAGMA table_info(messages)")
	if err != nil {
		return fmt.Errorf("failed to inspect messages table: %v", err)
	}
	existing := map[string]bool{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			return fmt.Errorf("failed to inspect messages table: %v", err)
		}
		existing[name] = true
	}
	rows.Close()

	for _, column := range deletionColumns {
		if existing[column.name] {
			continue
		}
		if _, err := db.Exec(fmt.Sprintf("ALTER TABLE messages ADD COLUMN %s %s", column.name, column.definition)); err != nil {
			return fmt.Errorf("failed to add column %s: %v", column.name, err)
		}
	}
	return nil
}

// EnsureChat creates the chat row if it is missing, without moving its last
// message time: a deletion is not new conversation activity.
func (store *MessageStore) EnsureChat(jid, name string, lastMessageTime time.Time) error {
	_, err := store.db.Exec(
		"INSERT OR IGNORE INTO chats (jid, name, last_message_time) VALUES (?, ?, ?)",
		jid, name, lastMessageTime,
	)
	return err
}

// deletedMessagePlaceholder is the content of a tombstone: a message deleted
// before the bridge stored it. Readers that do not know about the deletion
// columns still render something meaningful instead of an empty message.
const deletedMessagePlaceholder = "[deleted message]"

// DeletedMessage is what is known about a message after it was marked as
// deleted. Content is deletedMessagePlaceholder when the bridge never stored
// the original.
type DeletedMessage struct {
	ID        string
	ChatJID   string
	Sender    string
	Content   string
	Timestamp time.Time
	IsFromMe  bool
	MediaType string
	Filename  string
}

// MarkMessageDeleted flags a message as deleted, keeping its content. When the
// original was never stored (sent before the bridge ran, or dropped), a
// tombstone row is created so the deletion still shows up in the chat.
// "Delete for everyone" is never downgraded by a later "delete for me".
func (store *MessageStore) MarkMessageDeleted(id, chatJID, sender string, timestamp time.Time, isFromMe bool, deletion MessageDeletion) (DeletedMessage, error) {
	_, err := store.db.Exec(
		`INSERT INTO messages
		(id, chat_jid, sender, content, timestamp, is_from_me, media_type, filename, deleted_at, deleted_by, delete_scope)
		VALUES (?, ?, ?, ?, ?, ?, '', '', ?, ?, ?)
		ON CONFLICT(id, chat_jid) DO UPDATE SET
			deleted_at = excluded.deleted_at,
			deleted_by = excluded.deleted_by,
			delete_scope = excluded.delete_scope
		WHERE messages.delete_scope IS NULL OR messages.delete_scope != ?`,
		id, chatJID, sender, deletedMessagePlaceholder, timestamp, isFromMe, deletion.DeletedAt, deletion.DeletedBy, deletion.Scope, DeleteScopeEveryone,
	)
	if err != nil {
		return DeletedMessage{}, err
	}

	deleted := DeletedMessage{ID: id, ChatJID: chatJID}
	var content, mediaType, filename sql.NullString
	err = store.db.QueryRow(
		"SELECT sender, content, timestamp, is_from_me, media_type, filename FROM messages WHERE id = ? AND chat_jid = ?",
		id, chatJID,
	).Scan(&deleted.Sender, &content, &deleted.Timestamp, &deleted.IsFromMe, &mediaType, &filename)
	deleted.Content, deleted.MediaType, deleted.Filename = content.String, mediaType.String, filename.String
	return deleted, err
}

// MarkChatDeleted flags every message of a chat that is not already deleted.
func (store *MessageStore) MarkChatDeleted(chatJID string, deletion MessageDeletion) (int64, error) {
	result, err := store.db.Exec(
		`UPDATE messages SET deleted_at = ?, deleted_by = ?, delete_scope = ?
		WHERE chat_jid = ? AND deleted_at IS NULL`,
		deletion.DeletedAt, deletion.DeletedBy, deletion.Scope, chatJID,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// GetMessageDeletion returns how a message was deleted, or nil if it was not.
func (store *MessageStore) GetMessageDeletion(id, chatJID string) (*MessageDeletion, error) {
	var deletedAt sql.NullTime
	var deletedBy, scope sql.NullString
	err := store.db.QueryRow(
		"SELECT deleted_at, deleted_by, delete_scope FROM messages WHERE id = ? AND chat_jid = ?",
		id, chatJID,
	).Scan(&deletedAt, &deletedBy, &scope)
	if err != nil || !deletedAt.Valid {
		return nil, err
	}
	return &MessageDeletion{DeletedAt: deletedAt.Time, DeletedBy: deletedBy.String, Scope: scope.String}, nil
}

func ownUser(client *whatsmeow.Client) string {
	if client == nil || client.Store == nil || client.Store.ID == nil {
		return ""
	}
	return client.Store.ID.User
}

// revokedMessageSender resolves who sent the message a revoke points to: the
// key is the original message's key, not the revoke's.
func revokedMessageSender(key interface {
	GetFromMe() bool
	GetParticipant() string
}, chat types.JID, ownUser string) string {
	if key.GetFromMe() {
		return ownUser
	}
	if participant := key.GetParticipant(); participant != "" {
		if jid, err := types.ParseJID(participant); err == nil {
			return jid.User
		}
	}
	return chat.User
}

func deletionEvent(deleted DeletedMessage, chatName string, deletion MessageDeletion) MessageEvent {
	return MessageEvent{
		Type:      MessageEventTypeDeleted,
		ID:        deleted.ID,
		ChatJID:   deleted.ChatJID,
		ChatName:  chatName,
		Sender:    deleted.Sender,
		Content:   deleted.Content,
		Timestamp: deleted.Timestamp,
		IsFromMe:  deleted.IsFromMe,
		MediaType: deleted.MediaType,
		Filename:  deleted.Filename,
		Deletion:  &deletion,
	}
}

// handleRevoke records a "delete for everyone" and reports whether msg was a
// revoke, in which case it must not be stored as a message of its own.
func handleRevoke(client *whatsmeow.Client, messageStore *MessageStore, broker *EventBroker, msg *events.Message, chatName string, logger waLog.Logger) bool {
	protocol := unwrapMessage(msg.Message).GetProtocolMessage()
	if protocol.GetType() != waProto.ProtocolMessage_REVOKE || protocol.GetKey().GetID() == "" {
		return false
	}

	chatJID := msg.Info.Chat.String()
	own := ownUser(client)
	deletion := MessageDeletion{
		DeletedAt: msg.Info.Timestamp,
		DeletedBy: msg.Info.Sender.User,
		Scope:     DeleteScopeEveryone,
		ByMe:      msg.Info.IsFromMe,
	}
	key := protocol.GetKey()
	sender := revokedMessageSender(key, msg.Info.Chat, own)

	if err := messageStore.EnsureChat(chatJID, chatName, msg.Info.Timestamp); err != nil {
		logger.Warnf("Failed to store chat: %v", err)
	}
	deleted, err := messageStore.MarkMessageDeleted(key.GetID(), chatJID, sender, msg.Info.Timestamp, key.GetFromMe(), deletion)
	if err != nil {
		logger.Warnf("Failed to mark message %s as deleted: %v", key.GetID(), err)
		return true
	}

	broker.Publish(deletionEvent(deleted, chatName, deletion))
	fmt.Printf("[%s] %s deleted message %s in %s for everyone\n",
		msg.Info.Timestamp.Format("2006-01-02 15:04:05"), deletion.DeletedBy, key.GetID(), chatJID)
	return true
}

// handleDeleteForMe records a "delete for me" done on another device.
func handleDeleteForMe(client *whatsmeow.Client, messageStore *MessageStore, broker *EventBroker, evt *events.DeleteForMe, logger waLog.Logger) {
	chatJID := evt.ChatJID.String()
	own := ownUser(client)
	sender := evt.ChatJID.User
	switch {
	case evt.IsFromMe:
		sender = own
	case !evt.SenderJID.IsEmpty():
		sender = evt.SenderJID.User
	}
	deletion := MessageDeletion{DeletedAt: evt.Timestamp, DeletedBy: own, Scope: DeleteScopeMe, ByMe: true}

	chatName := GetChatName(client, messageStore, evt.ChatJID, chatJID, nil, sender, logger)
	if err := messageStore.EnsureChat(chatJID, chatName, evt.Timestamp); err != nil {
		logger.Warnf("Failed to store chat: %v", err)
	}
	deleted, err := messageStore.MarkMessageDeleted(evt.MessageID, chatJID, sender, evt.Timestamp, evt.IsFromMe, deletion)
	if err != nil {
		logger.Warnf("Failed to mark message %s as deleted: %v", evt.MessageID, err)
		return
	}
	broker.Publish(deletionEvent(deleted, chatName, deletion))
}

// handleChatDeletion records a chat deleted or cleared on another device.
func handleChatDeletion(client *whatsmeow.Client, messageStore *MessageStore, broker *EventBroker, chat types.JID, timestamp time.Time, scope string, logger waLog.Logger) {
	chatJID := chat.String()
	deletion := MessageDeletion{DeletedAt: timestamp, DeletedBy: ownUser(client), Scope: scope, ByMe: true}

	count, err := messageStore.MarkChatDeleted(chatJID, deletion)
	if err != nil {
		logger.Warnf("Failed to mark chat %s as deleted: %v", chatJID, err)
		return
	}
	chatName := GetChatName(client, messageStore, chat, chatJID, nil, "", logger)
	broker.Publish(MessageEvent{
		Type:      MessageEventTypeDeleted,
		ChatJID:   chatJID,
		ChatName:  chatName,
		Timestamp: timestamp,
		Deletion:  &deletion,
	})
	fmt.Printf("Chat %s %s on another device; marked %d messages\n", chatJID, scope, count)
}

// recordHistoryRevoke marks a message that history sync reports as deleted for
// everyone. The stub carries the original key and timestamp but not who
// deleted it, so the original sender is assumed.
func recordHistoryRevoke(client *whatsmeow.Client, messageStore *MessageStore, webMsg *waProto.WebMessageInfo, chat types.JID, logger waLog.Logger) bool {
	id := webMsg.GetKey().GetID()
	if id == "" || webMsg.GetMessageTimestamp() == 0 {
		return false
	}
	own := ownUser(client)
	sender := revokedMessageSender(webMsg.GetKey(), chat, own)
	if participant := webMsg.GetParticipant(); participant != "" && !webMsg.GetKey().GetFromMe() {
		if jid, err := types.ParseJID(participant); err == nil {
			sender = jid.User
		}
	}
	timestamp := time.Unix(int64(webMsg.GetMessageTimestamp()), 0)
	deletion := MessageDeletion{
		DeletedAt: timestamp,
		DeletedBy: sender,
		Scope:     DeleteScopeEveryone,
		ByMe:      webMsg.GetKey().GetFromMe(),
	}
	if _, err := messageStore.MarkMessageDeleted(id, chat.String(), sender, timestamp, webMsg.GetKey().GetFromMe(), deletion); err != nil {
		logger.Warnf("Failed to mark history message %s as deleted: %v", id, err)
		return false
	}
	return true
}
