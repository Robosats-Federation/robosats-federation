from datetime import timedelta

from django.urls import reverse
from django.utils import timezone

from api.models import Order
from tests.pipeline.base import PipelineBaseTest
from tests.utils.trade import Trade, maker_form_buy_with_range


class TestDisputeCooldown(PipelineBaseTest):
    def test_cannot_dispute_immediately_after_entering_chat(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.take_order()
        trade.take_order_third()
        trade.lock_taker_bond()
        trade.lock_escrow(trade.taker_index)
        trade.submit_payout_invoice(trade.maker_index)

        path = reverse("order")
        params = f"?order_id={trade.order_id}"
        headers = trade.get_robot_auth(trade.maker_index)
        response = self.client.post(path + params, {"action": "dispute"}, **headers)

        self.assertEqual(response.status_code, 400)

    def test_can_dispute_after_cooldown_expired(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.take_order()
        trade.take_order_third()
        trade.lock_taker_bond()
        trade.lock_escrow(trade.taker_index)
        trade.submit_payout_invoice(trade.maker_index)

        # Set expires_at so that enableDisputeTime (expires_at - 18h) is 1h in the past:
        #   expires_at = now + 17h  →  enableDisputeTime = now - 1h  → cooldown expired
        # The order itself is NOT expired (17h remaining).
        order = Order.objects.get(id=trade.order_id)
        order.expires_at = timezone.now() + timedelta(hours=17)
        order.save()

        path = reverse("order")
        params = f"?order_id={trade.order_id}"
        headers = trade.get_robot_auth(trade.maker_index)
        response = self.client.post(path + params, {"action": "dispute"}, **headers)

        self.assertEqual(response.status_code, 200)
        data = response.json()
        self.assertEqual(data["status"], Order.Status.DIS)
