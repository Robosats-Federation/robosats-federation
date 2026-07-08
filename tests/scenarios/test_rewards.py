from django.urls import reverse

from tests.pipeline.base import PipelineBaseTest
from tests.utils.node import add_invoice
from tests.utils.pgp import sign_message
from tests.utils.trade import Trade, maker_form_buy_with_range


class TestRewards(PipelineBaseTest):
    def test_withdraw_reward_after_unilateral_cancel(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.take_order()
        trade.take_order_third()
        trade.lock_taker_bond()
        trade.cancel_order(trade.maker_index)

        path = reverse("robot")
        taker_headers = trade.get_robot_auth(trade.taker_index)
        response = self.client.get(path, **taker_headers)
        self.assertEqual(response.status_code, 200)
        self.assertResponse(response)
        self.assertIsInstance(response.json()["earned_rewards"], int)

        path = reverse("reward")
        invoice = add_invoice("robot", response.json()["earned_rewards"])
        signed_payout_invoice = sign_message(
            invoice,
            passphrase_path=f"tests/robots/{trade.taker_index}/token",
            private_key_path=f"tests/robots/{trade.taker_index}/enc_priv_key",
        )
        body = {"invoice": signed_payout_invoice}
        response = self.client.post(path, body, **taker_headers)
        self.assertEqual(response.status_code, 200)
        self.assertResponse(response)
        self.assertTrue(response.json()["successful_withdrawal"])

    def test_withdraw_reward_after_unilateral_cancel_routing_budget(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.take_order()
        trade.take_order_third()
        trade.lock_taker_bond()
        trade.cancel_order(trade.maker_index)

        path = reverse("robot")
        taker_headers = trade.get_robot_auth(trade.taker_index)
        response = self.client.get(path, **taker_headers)
        self.assertEqual(response.status_code, 200)
        self.assertResponse(response)
        self.assertIsInstance(response.json()["earned_rewards"], int)

        path = reverse("reward")
        invoice = add_invoice("robot", response.json()["earned_rewards"])
        signed_payout_invoice = sign_message(
            invoice,
            passphrase_path=f"tests/robots/{trade.taker_index}/token",
            private_key_path=f"tests/robots/{trade.taker_index}/enc_priv_key",
        )
        body = {"invoice": signed_payout_invoice, "routing_budget_ppm": 0}
        response = self.client.post(path, body, **taker_headers)
        self.assertEqual(response.status_code, 200)
        self.assertResponse(response)
        self.assertTrue(response.json()["successful_withdrawal"])
