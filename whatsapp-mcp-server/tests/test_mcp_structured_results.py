import sys
import unittest
from datetime import datetime, timezone
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import main
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


if __name__ == "__main__":
    unittest.main()
