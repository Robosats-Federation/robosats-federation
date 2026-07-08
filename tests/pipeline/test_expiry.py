from datetime import datetime

from django.urls import reverse

from api.models import Order
from tests.pipeline.base import PipelineBaseTest
from tests.utils.trade import Trade


class ExpiryTest(PipelineBaseTest):
    def test_created_order_expires(self):
        trade = Trade(self.client)

        order = Order.objects.get(id=trade.response.json()["id"])
        order.expires_at = datetime.now()
        order.save()

        trade.clean_orders()

        trade.get_order()
        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)

        self.assertEqual(
            data["status"],
            Order.Status.EXP,
        )
        self.assertEqual(
            data["expiry_message"],
            Order.ExpiryReasons(Order.ExpiryReasons.NMBOND).label,
        )
        self.assertEqual(data["expiry_reason"], Order.ExpiryReasons.NMBOND)

        self.assert_order_logs(data["id"])

    def test_public_order_expires(self):
        trade = Trade(self.client)
        trade.publish_order()
        trade.expire_order()

        trade.clean_orders()

        trade.get_order()
        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)

        self.assertEqual(
            data["status"],
            Order.Status.EXP,
        )
        self.assertEqual(
            data["expiry_message"],
            Order.ExpiryReasons(Order.ExpiryReasons.NTAKEN).label,
        )
        self.assertEqual(data["expiry_reason"], Order.ExpiryReasons.NTAKEN)

        self.assert_order_logs(data["id"])

        maker_headers = trade.get_robot_auth(trade.maker_index)
        response = self.client.get(reverse("notifications"), **maker_headers)
        self.assertResponse(response)
        notifications_data = list(response.json())
        self.assertEqual(notifications_data[0]["order_id"], trade.order_id)
        self.assertEqual(
            notifications_data[0]["title"],
            f"😪 Hey {data['maker_nick']}, your order with ID {str(trade.order_id)} has expired without a taker.",
        )

        trade.get_review(trade.maker_index)
        self.assertEqual(trade.response.status_code, 400)

    def test_taken_order_expires(self):
        trade = Trade(self.client)
        trade.publish_order()
        trade.take_order()
        trade.take_order_third()
        trade.lock_taker_bond()

        trade.expire_order()

        trade.clean_orders()

        trade.get_order()
        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)

        self.assertEqual(
            data["status"],
            Order.Status.EXP,
        )
        self.assertEqual(
            data["expiry_message"],
            Order.ExpiryReasons(Order.ExpiryReasons.NESINV).label,
        )
        self.assertEqual(data["expiry_reason"], Order.ExpiryReasons.NESINV)

        self.assert_order_logs(data["id"])

    def test_escrow_locked_expires(self):
        trade = Trade(self.client)
        trade.publish_order()
        trade.take_order()
        trade.take_order_third()
        trade.lock_taker_bond()
        trade.lock_escrow(trade.taker_index)

        order = Order.objects.get(id=trade.response.json()["id"])
        order.expires_at = datetime.now()
        order.save()

        trade.clean_orders()

        trade.get_order()
        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)

        self.assertEqual(
            data["status"],
            Order.Status.EXP,
        )
        self.assertEqual(
            data["expiry_message"],
            Order.ExpiryReasons(Order.ExpiryReasons.NINVOI).label,
        )
        self.assertEqual(data["expiry_reason"], Order.ExpiryReasons.NINVOI)

        self.assert_order_logs(data["id"])
