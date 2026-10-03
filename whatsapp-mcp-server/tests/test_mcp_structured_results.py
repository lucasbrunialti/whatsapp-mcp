import sqlite3
import sys
import tempfile
import unittest
from datetime import datetime, timezone
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import main
import whatsapp
from whatsapp import Message


class MCPStructuredResultsTest(unittest.TestCase):
    def test_list_messages_returns_json_safe_dicts_for_dataclass_messages(self):
        message = Message(
            timestamp=datetime(2026, 9, 2, 12, 30, tzinfo=timezone.utc),
            sender="5511999999999",
            content="Horário disponível às 14h",
            is_from_me=False,
            chat_jid="5511999999999@s.whatsapp.net",
            id="message-1",
        )

        with patch.object(main, "whatsapp_list_messages", return_value=[message]):
            result = main.list_messages(chat_jid=message.chat_jid)

        self.assertEqual(
            result,
            [
                {
                    "timestamp": "2026-09-02T12:30:00+00:00",
                    "sender": "5511999999999",
                    "content": "Horário disponível às 14h",
                    "is_from_me": False,
                    "chat_jid": "5511999999999@s.whatsapp.net",
                    "id": "message-1",
                    "chat_name": None,
                    "media_type": None,
                    "deleted_at": None,
                    "deleted_by": None,
                    "delete_scope": None,
                }
            ],
        )

    def test_raw_list_messages_returns_messages_not_rendered_text(self):
        with tempfile.NamedTemporaryFile(suffix=".db") as db_file:
            connection = sqlite3.connect(db_file.name)
            connection.executescript(
                """
                CREATE TABLE chats (jid TEXT PRIMARY KEY, name TEXT);
                CREATE TABLE messages (
                    id TEXT PRIMARY KEY,
                    chat_jid TEXT,
                    sender TEXT,
                    content TEXT,
                    timestamp TEXT,
                    is_from_me BOOLEAN,
                    media_type TEXT
                );
                INSERT INTO chats VALUES ('chat@g.us', 'Grupo');
                INSERT INTO messages VALUES (
                    'message-1', 'chat@g.us', '5511999999999', 'Flávio entrou',
                    '2026-09-03T20:00:00+00:00', 0, NULL
                );
                """
            )
            connection.commit()
            connection.close()

            with patch.object(whatsapp, "MESSAGES_DB_PATH", db_file.name):
                result = whatsapp.list_messages(
                    chat_jid="chat@g.us", include_context=False
                )

        self.assertIsInstance(result, list)
        self.assertEqual(len(result), 1)
        self.assertEqual(result[0].content, "Flávio entrou")
        # A database the bridge has not migrated yet has no deletion columns.
        self.assertIsNone(result[0].deleted_at)

    def _deleted_message_db(self, path):
        connection = sqlite3.connect(path)
        connection.executescript(
            """
            CREATE TABLE chats (jid TEXT PRIMARY KEY, name TEXT);
            CREATE TABLE messages (
                id TEXT,
                chat_jid TEXT,
                sender TEXT,
                content TEXT,
                timestamp TIMESTAMP,
                is_from_me BOOLEAN,
                media_type TEXT,
                deleted_at TIMESTAMP,
                deleted_by TEXT,
                delete_scope TEXT,
                PRIMARY KEY (id, chat_jid)
            );
            INSERT INTO chats VALUES ('5511999999999@s.whatsapp.net', 'Fornecedor');
            INSERT INTO messages VALUES (
                'before', '5511999999999@s.whatsapp.net', '5511999999999', 'Bom dia',
                '2026-09-03 08:00:00+00:00', 0, '', NULL, NULL, NULL
            );
            INSERT INTO messages VALUES (
                'deleted', '5511999999999@s.whatsapp.net', '5511999999999', 'Preço final: R$ 1.200',
                '2026-09-03 09:00:00+00:00', 0, '',
                '2026-09-03 10:00:00+00:00', '5511999999999', 'everyone'
            );
            """
        )
        connection.commit()
        connection.close()

    def test_list_messages_exposes_deletion_and_keeps_content(self):
        with tempfile.NamedTemporaryFile(suffix=".db") as db_file:
            self._deleted_message_db(db_file.name)
            with patch.object(whatsapp, "MESSAGES_DB_PATH", db_file.name):
                result = whatsapp.list_messages(
                    chat_jid="5511999999999@s.whatsapp.net", include_context=False
                )
                structured = main.list_messages(
                    chat_jid="5511999999999@s.whatsapp.net", include_context=False
                )

        deleted = next(message for message in result if message.id == "deleted")
        self.assertEqual(deleted.content, "Preço final: R$ 1.200")
        self.assertTrue(deleted.is_deleted)
        self.assertEqual(deleted.delete_scope, "everyone")
        self.assertEqual(deleted.deleted_by, "5511999999999")
        self.assertEqual(deleted.deleted_at, datetime(2026, 9, 3, 10, 0, tzinfo=timezone.utc))
        self.assertFalse(next(m for m in result if m.id == "before").is_deleted)

        deleted_dict = next(message for message in structured if message["id"] == "deleted")
        self.assertEqual(deleted_dict["deleted_at"], "2026-09-03T10:00:00+00:00")
        self.assertEqual(deleted_dict["delete_scope"], "everyone")

    def test_message_context_and_rendering_show_deletion(self):
        with tempfile.NamedTemporaryFile(suffix=".db") as db_file:
            self._deleted_message_db(db_file.name)
            with patch.object(whatsapp, "MESSAGES_DB_PATH", db_file.name):
                context = whatsapp.get_message_context("deleted", before=1, after=1)
                rendered = whatsapp.format_message(context.message)

        self.assertTrue(context.message.is_deleted)
        self.assertEqual([m.id for m in context.before], ["before"])
        self.assertIn("[deleted for everyone by Fornecedor at 2026-09-03 10:00:00]", rendered)
        self.assertIn("Preço final: R$ 1.200", rendered)


if __name__ == "__main__":
    unittest.main()
