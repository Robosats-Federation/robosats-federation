from django.urls import reverse

from api.models import Order
from tests.pipeline.base import PipelineBaseTest
from tests.utils.trade import Trade, maker_form_buy_with_range, read_file


class BuyConfirmPayoutTest(PipelineBaseTest):
    def test_trade_to_submitted_address(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.take_order()
        trade.take_order_third()
        trade.lock_taker_bond()
        trade.lock_escrow(trade.taker_index)
        trade.submit_payout_address(trade.maker_index)

        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertEqual(data["status_message"], Order.Status(Order.Status.CHA).label)
        self.assertFalse(data["is_fiat_sent"])

        maker_headers = trade.get_robot_auth(trade.maker_index)
        response = self.client.get(reverse("notifications"), **maker_headers)
        self.assertResponse(response)
        notifications_data = list(response.json())
        self.assertEqual(notifications_data[0]["order_id"], trade.order_id)
        self.assertEqual(
            notifications_data[0]["title"],
            f"✅ Hey {data['maker_nick']}, the escrow and invoice have been submitted. The fiat exchange starts now via the platform chat.",
        )
        taker_headers = trade.get_robot_auth(trade.taker_index)
        response = self.client.get(reverse("notifications"), **taker_headers)
        self.assertResponse(response)
        notifications_data = list(response.json())
        self.assertEqual(notifications_data[0]["order_id"], trade.order_id)
        self.assertEqual(
            notifications_data[0]["title"],
            f"✅ Hey {data['taker_nick']}, the escrow and invoice have been submitted. The fiat exchange starts now via the platform chat.",
        )

        trade.cancel_order(trade.maker_index)
        trade.cancel_order(trade.taker_index)
        self.assert_order_logs(data["id"])

    def test_trade_to_submitted_invoice(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.take_order()
        trade.take_order_third()
        trade.lock_taker_bond()
        trade.lock_escrow(trade.taker_index)
        trade.submit_payout_invoice(trade.maker_index)

        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertEqual(data["status_message"], Order.Status(Order.Status.CHA).label)
        self.assertFalse(data["is_fiat_sent"])

        maker_headers = trade.get_robot_auth(trade.maker_index)
        maker_nick = read_file(f"tests/robots/{trade.maker_index}/nickname")
        response = self.client.get(reverse("notifications"), **maker_headers)
        self.assertResponse(response)
        notifications_data = list(response.json())
        self.assertEqual(notifications_data[0]["order_id"], trade.order_id)
        self.assertEqual(
            notifications_data[0]["title"],
            f"✅ Hey {maker_nick}, the escrow and invoice have been submitted. The fiat exchange starts now via the platform chat.",
        )
        taker_headers = trade.get_robot_auth(trade.taker_index)
        taker_nick = read_file(f"tests/robots/{trade.taker_index}/nickname")
        response = self.client.get(reverse("notifications"), **taker_headers)
        self.assertResponse(response)
        notifications_data = list(response.json())
        self.assertEqual(notifications_data[0]["order_id"], trade.order_id)
        self.assertEqual(
            notifications_data[0]["title"],
            f"✅ Hey {taker_nick}, the escrow and invoice have been submitted. The fiat exchange starts now via the platform chat.",
        )

        trade.cancel_order(trade.maker_index)
        trade.cancel_order(trade.taker_index)

    def test_trade_to_confirm_fiat_sent_LN(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.take_order()
        trade.take_order_third()
        trade.lock_taker_bond()
        trade.lock_escrow(trade.taker_index)
        trade.submit_payout_invoice(trade.maker_index)
        trade.confirm_fiat(trade.maker_index)

        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertEqual(data["status_message"], Order.Status(Order.Status.FSE).label)
        self.assertTrue(data["is_fiat_sent"])

        trade.undo_confirm_sent(trade.maker_index)
        data = trade.response.json()
        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertEqual(data["status_message"], Order.Status(Order.Status.CHA).label)

        trade.cancel_order(trade.maker_index)
        trade.cancel_order(trade.taker_index)
        self.assert_order_logs(data["id"])

    def test_trade_to_confirm_fiat_received_LN(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.take_order()
        trade.take_order_third()
        trade.lock_taker_bond()
        trade.lock_escrow(trade.taker_index)
        trade.submit_payout_invoice(trade.maker_index)
        trade.confirm_fiat(trade.maker_index)
        trade.confirm_fiat(trade.taker_index)

        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertEqual(data["status_message"], Order.Status(Order.Status.PAY).label)
        self.assertTrue(data["is_fiat_sent"])
        self.assertFalse(data["is_disputed"])
        self.assertFalse(data["maker_locked"])
        self.assertFalse(data["taker_locked"])
        self.assertFalse(data["escrow_locked"])
        self.assert_order_logs(data["id"])

        maker_headers = trade.get_robot_auth(trade.maker_index)
        response = self.client.get(reverse("notifications"), **maker_headers)
        self.assertResponse(response)
        notifications_data = list(response.json())
        self.assertEqual(notifications_data[0]["order_id"], trade.order_id)
        self.assertEqual(
            notifications_data[0]["title"],
            f"🥳 Your order with ID {str(trade.order_id)} has finished successfully!",
        )
        taker_headers = trade.get_robot_auth(trade.taker_index)
        response = self.client.get(reverse("notifications"), **taker_headers)
        self.assertResponse(response)
        notifications_data = list(response.json())
        self.assertEqual(notifications_data[0]["order_id"], trade.order_id)
        self.assertEqual(
            notifications_data[0]["title"],
            f"🥳 Your order with ID {str(trade.order_id)} has finished successfully!",
        )

    def test_successful_LN(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.take_order()
        trade.take_order_third()
        trade.lock_taker_bond()
        trade.lock_escrow(trade.taker_index)
        trade.submit_payout_invoice(trade.maker_index)
        trade.confirm_fiat(trade.maker_index)
        trade.confirm_fiat(trade.taker_index)

        trade.process_payouts()
        trade.get_order(trade.maker_index)

        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertEqual(data["status_message"], Order.Status(Order.Status.SUC).label)
        self.assertTrue(data["is_fiat_sent"])
        self.assertFalse(data["is_disputed"])
        self.assertIsHash(data["maker_summary"]["preimage"])
        self.assertIsHash(data["maker_summary"]["payment_hash"])
        self.assert_order_logs(data["id"])

    def test_successful_onchain(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.take_order()
        trade.take_order_third()
        trade.lock_taker_bond()
        trade.lock_escrow(trade.taker_index)
        trade.submit_payout_address(trade.maker_index)
        trade.confirm_fiat(trade.maker_index)
        trade.confirm_fiat(trade.taker_index)

        trade.process_payouts(mine_a_block=True)
        trade.get_order(trade.maker_index)

        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertEqual(data["status_message"], Order.Status(Order.Status.SUC).label)
        self.assertTrue(data["is_fiat_sent"])
        self.assertFalse(data["is_disputed"])
        self.assertIsInstance(data["maker_summary"]["address"], str)
        self.assertIsHash(data["maker_summary"]["txid"])
        self.assert_order_logs(data["id"])

    def test_review_order(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()

        trade.get_review()
        self.assertEqual(trade.response.status_code, 400)

        trade.take_order()
        trade.take_order_third()
        trade.lock_taker_bond()

        trade.get_review(trade.maker_index)
        self.assertEqual(trade.response.status_code, 400)
        trade.get_review(trade.taker_index)
        self.assertEqual(trade.response.status_code, 400)

        trade.lock_escrow(trade.taker_index)
        trade.submit_payout_address(trade.maker_index)
        trade.confirm_fiat(trade.maker_index)
        trade.confirm_fiat(trade.taker_index)

        trade.process_payouts(mine_a_block=True)

        trade.get_review(trade.maker_index)
        self.assertEqual(trade.response.status_code, 200)
        nostr_pubkey = read_file(f"tests/robots/{trade.maker_index}/nostr_pubkey")
        data = trade.response.json()
        self.assertEqual(data["pubkey"], nostr_pubkey)
        self.assertIsInstance(data["token"], str)

        trade.get_review(trade.taker_index)
        self.assertEqual(trade.response.status_code, 200)
        nostr_pubkey = read_file(f"tests/robots/{trade.taker_index}/nostr_pubkey")
        data = trade.response.json()
        self.assertEqual(data["pubkey"], nostr_pubkey)
        self.assertIsInstance(data["token"], str)

    def test_lightning_payment_failed(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.take_order()
        trade.take_order_third()
        trade.lock_taker_bond()
        trade.lock_escrow(trade.taker_index)
        trade.submit_payout_invoice(trade.maker_index)

        trade.change_order_status(Order.Status.FAI)
        trade.clean_orders()

        maker_headers = trade.get_robot_auth(trade.maker_index)
        maker_nick = read_file(f"tests/robots/{trade.maker_index}/nickname")
        response = self.client.get(reverse("notifications"), **maker_headers)
        self.assertResponse(response)
        notifications_data = list(response.json())
        self.assertEqual(notifications_data[0]["order_id"], trade.order_id)
        self.assertEqual(
            notifications_data[0]["title"],
            f"⚡❌ Hey {maker_nick}, the lightning payment on your order with ID {str(trade.order_id)} failed.",
        )
