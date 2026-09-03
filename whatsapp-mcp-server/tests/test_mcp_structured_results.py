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


if __name__ == "__main__":
    unittest.main()
