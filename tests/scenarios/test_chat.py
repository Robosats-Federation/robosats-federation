from django.urls import reverse

from tests.pipeline.base import PipelineBaseTest
from tests.utils.trade import Trade, maker_form_buy_with_range, read_file


class TestChat(PipelineBaseTest):
    def test_chat_messages(self):
        path = reverse("chat")
        message = (
            "Example message string. Note clients will verify expect only PGP messages."
        )

        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.take_order()
        trade.take_order_third()
        trade.lock_taker_bond()
        trade.lock_escrow(trade.taker_index)
        trade.submit_payout_invoice(trade.maker_index)

        params = f"?order_id={trade.order_id}"
        maker_headers = trade.get_robot_auth(trade.maker_index)
        taker_headers = trade.get_robot_auth(trade.taker_index)
        maker_nick = read_file(f"tests/robots/{trade.maker_index}/nickname")
        taker_nick = read_file(f"tests/robots/{trade.taker_index}/nickname")

        response = self.client.get(path + params, **maker_headers)
        self.assertResponse(response)
        self.assertEqual(response.status_code, 200)
        self.assertEqual(response.json()["messages"], [])
        self.assertTrue(response.json()["peer_connected"])

        response = self.client.get(path + params, **taker_headers)
        self.assertResponse(response)
        self.assertEqual(response.status_code, 200)
        self.assertEqual(response.json()["messages"], [])
        self.assertTrue(response.json()["peer_connected"])

        trade.send_chat_message(message, trade.maker_index)
        self.assertResponse(trade.response)
        self.assertEqual(trade.response.status_code, 200)
        self.assertEqual(trade.response.json()["messages"][0]["message"], message)
        self.assertTrue(trade.response.json()["peer_connected"])

        taker_headers = trade.get_robot_auth(trade.taker_index)
        response = self.client.get(reverse("notifications"), **taker_headers)
        self.assertResponse(response)
        notifications_data = list(response.json())
        self.assertEqual(notifications_data[0]["order_id"], trade.order_id)
        self.assertEqual(
            notifications_data[0]["title"],
            f"💬 Hey {taker_nick}, a new chat message in-app was sent to you by {maker_nick} for order ID {trade.order_id}.",
        )

        trade.send_chat_message(message + " 2", trade.taker_index)
        self.assertResponse(trade.response)
        self.assertEqual(trade.response.status_code, 200)
        self.assertEqual(trade.response.json()["messages"][0]["message"], message)
        self.assertEqual(
            trade.response.json()["messages"][1]["message"], message + " 2"
        )

        maker_headers = trade.get_robot_auth(trade.maker_index)
        response = self.client.get(reverse("notifications"), **maker_headers)
        self.assertResponse(response)
        notifications_data = list(response.json())
        self.assertEqual(notifications_data[0]["order_id"], trade.order_id)
        self.assertEqual(
            notifications_data[0]["title"],
            f"✅ Hey {maker_nick}, the escrow and invoice have been submitted. The fiat exchange starts now via the platform chat.",
        )

        response = self.client.get(path + params, **maker_headers)
        self.assertResponse(response)
        self.assertEqual(response.status_code, 200)
        self.assertTrue(response.json()["peer_connected"])
        self.assertEqual(response.json()["messages"][0]["message"], message)
        self.assertEqual(response.json()["messages"][1]["message"], message + " 2")
        self.assertEqual(response.json()["messages"][0]["nick"], maker_nick)
        self.assertEqual(response.json()["messages"][1]["nick"], taker_nick)

        trade.cancel_order(trade.maker_index)
        trade.cancel_order(trade.taker_index)
