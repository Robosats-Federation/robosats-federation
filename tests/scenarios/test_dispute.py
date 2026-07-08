from datetime import datetime, timedelta
from unittest.mock import MagicMock

from django.contrib.admin.sites import AdminSite
from django.http import HttpRequest
from django.urls import reverse

from api.admin import OrderAdmin
from api.models import Order
from django.utils import timezone
from tests.pipeline.base import PipelineBaseTest
from tests.utils.trade import Trade, maker_form_buy_with_range


class TestDispute(PipelineBaseTest):
    def test_expires_after_only_taker_messaged(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.take_order()
        trade.take_order_third()
        trade.lock_taker_bond()
        trade.lock_escrow(trade.taker_index)
        trade.submit_payout_invoice(trade.maker_index)

        path = reverse("chat")
        message = "Unencrypted message from taker"
        params = f"?order_id={trade.order_id}"
        taker_headers = trade.get_robot_auth(trade.taker_index)
        body = {"PGP_message": message, "order_id": trade.order_id}
        self.client.post(path + params, data=body, **taker_headers)

        order = Order.objects.get(id=trade.response.json()["id"])
        order.expires_at = datetime.now()
        order.save()
        trade.clean_orders()

        trade.get_order()
        data = trade.response.json()
        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertEqual(data["status"], Order.Status.MLD)
        self.assert_order_logs(data["id"])

        maker_headers = trade.get_robot_auth(trade.maker_index)
        response = self.client.get(reverse("notifications"), **maker_headers)
        self.assertResponse(response)
        notifications_data = list(response.json())
        self.assertEqual(notifications_data[0]["order_id"], trade.order_id)
        self.assertEqual(
            notifications_data[0]["title"],
            f"⚖️ Hey {data['maker_nick']}, a dispute has been opened on your order with ID {str(trade.order_id)}.",
        )
        taker_headers = trade.get_robot_auth(trade.taker_index)
        response = self.client.get(reverse("notifications"), **taker_headers)
        self.assertResponse(response)
        notifications_data = list(response.json())
        self.assertEqual(notifications_data[0]["order_id"], trade.order_id)
        self.assertEqual(
            notifications_data[0]["title"],
            f"⚖️ Hey {data['taker_nick']}, a dispute has been opened on your order with ID {str(trade.order_id)}.",
        )

    def test_expires_after_only_maker_messaged(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.take_order()
        trade.take_order_third()
        trade.lock_taker_bond()
        trade.lock_escrow(trade.taker_index)
        trade.submit_payout_invoice(trade.maker_index)

        path = reverse("chat")
        message = "Unencrypted message from maker"
        params = f"?order_id={trade.order_id}"
        maker_headers = trade.get_robot_auth(trade.maker_index)
        body = {"PGP_message": message, "order_id": trade.order_id}
        self.client.post(path + params, data=body, **maker_headers)

        order = Order.objects.get(id=trade.response.json()["id"])
        order.expires_at = datetime.now()
        order.save()
        trade.clean_orders()

        trade.get_order()
        data = trade.response.json()
        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertEqual(data["status"], Order.Status.TLD)
        self.assert_order_logs(data["id"])

        maker_headers = trade.get_robot_auth(trade.maker_index)
        response = self.client.get(reverse("notifications"), **maker_headers)
        self.assertResponse(response)
        notifications_data = list(response.json())
        self.assertEqual(notifications_data[0]["order_id"], trade.order_id)
        self.assertEqual(
            notifications_data[0]["title"],
            f"⚖️ Hey {data['maker_nick']}, a dispute has been opened on your order with ID {str(trade.order_id)}.",
        )
        taker_headers = trade.get_robot_auth(trade.taker_index)
        response = self.client.get(reverse("notifications"), **taker_headers)
        self.assertResponse(response)
        notifications_data = list(response.json())
        self.assertEqual(notifications_data[0]["order_id"], trade.order_id)
        self.assertEqual(
            notifications_data[0]["title"],
            f"⚖️ Hey {data['taker_nick']}, a dispute has been opened on your order with ID {str(trade.order_id)}.",
        )

    def test_expires_after_fiat_sent(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.take_order()
        trade.take_order_third()
        trade.lock_taker_bond()
        trade.lock_escrow(trade.taker_index)
        trade.submit_payout_address(trade.maker_index)
        trade.confirm_fiat(trade.maker_index)

        order = Order.objects.get(id=trade.response.json()["id"])
        order.expires_at = datetime.now()
        order.save()
        trade.clean_orders()

        trade.get_order()
        data = trade.response.json()
        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertEqual(data["status"], Order.Status.DIS)
        self.assert_order_logs(data["id"])

        maker_headers = trade.get_robot_auth(trade.maker_index)
        response = self.client.get(reverse("notifications"), **maker_headers)
        self.assertResponse(response)
        notifications_data = list(response.json())
        self.assertEqual(notifications_data[0]["order_id"], trade.order_id)
        self.assertEqual(
            notifications_data[0]["title"],
            f"⚖️ Hey {data['maker_nick']}, a dispute has been opened on your order with ID {str(trade.order_id)}.",
        )
        taker_headers = trade.get_robot_auth(trade.taker_index)
        response = self.client.get(reverse("notifications"), **taker_headers)
        self.assertResponse(response)
        notifications_data = list(response.json())
        self.assertEqual(notifications_data[0]["order_id"], trade.order_id)
        self.assertEqual(
            notifications_data[0]["title"],
            f"⚖️ Hey {data['taker_nick']}, a dispute has been opened on your order with ID {str(trade.order_id)}.",
        )

        trade.get_review(trade.maker_index)
        self.assertEqual(trade.response.status_code, 400)
        trade.get_review(trade.taker_index)
        self.assertEqual(trade.response.status_code, 400)

    def test_dispute_opened_manually(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.take_order()
        trade.take_order_third()
        trade.lock_taker_bond()
        trade.lock_escrow(trade.taker_index)
        trade.submit_payout_invoice(trade.maker_index)

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

    def test_dispute_submit_statements(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.take_order()
        trade.take_order_third()
        trade.lock_taker_bond()
        trade.lock_escrow(trade.taker_index)
        trade.submit_payout_invoice(trade.maker_index)

        order = Order.objects.get(id=trade.order_id)
        order.expires_at = timezone.now() + timedelta(hours=17)
        order.save()

        path = reverse("order")
        params = f"?order_id={trade.order_id}"

        maker_headers = trade.get_robot_auth(trade.maker_index)
        self.client.post(path + params, {"action": "dispute"}, **maker_headers)

        maker_statement = "M" * 100
        response = self.client.post(
            path + params,
            {"action": "submit_statement", "statement": maker_statement},
            **maker_headers,
        )
        self.assertEqual(response.status_code, 200)

        taker_headers = trade.get_robot_auth(trade.taker_index)
        taker_statement = "T" * 100
        response = self.client.post(
            path + params,
            {"action": "submit_statement", "statement": taker_statement},
            **taker_headers,
        )
        self.assertEqual(response.status_code, 200)

        trade.get_order()
        self.assertEqual(trade.response.json()["status"], Order.Status.WFR)

    def test_dispute_maker_wins_via_admin(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.take_order()
        trade.take_order_third()
        trade.lock_taker_bond()
        trade.lock_escrow(trade.taker_index)
        trade.submit_payout_invoice(trade.maker_index)

        order = Order.objects.get(id=trade.order_id)
        order.expires_at = timezone.now() + timedelta(hours=17)
        order.save()

        path = reverse("order")
        params = f"?order_id={trade.order_id}"
        maker_headers = trade.get_robot_auth(trade.maker_index)
        self.client.post(path + params, {"action": "dispute"}, **maker_headers)

        order_admin = OrderAdmin(model=Order, admin_site=AdminSite())
        order_admin.message_user = MagicMock()
        order = Order.objects.get(id=trade.order_id)
        order_admin.maker_wins(HttpRequest(), Order.objects.filter(id=order.id))

        order = Order.objects.get(id=trade.order_id)
        self.assertEqual(order.status, Order.Status.TLD)
        self.assertGreater(order.maker.robot.earned_rewards, 0)

    def test_dispute_taker_wins_via_admin(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.take_order()
        trade.take_order_third()
        trade.lock_taker_bond()
        trade.lock_escrow(trade.taker_index)
        trade.submit_payout_invoice(trade.maker_index)

        order = Order.objects.get(id=trade.order_id)
        order.expires_at = timezone.now() + timedelta(hours=17)
        order.save()

        path = reverse("order")
        params = f"?order_id={trade.order_id}"
        maker_headers = trade.get_robot_auth(trade.maker_index)
        self.client.post(path + params, {"action": "dispute"}, **maker_headers)

        order_admin = OrderAdmin(model=Order, admin_site=AdminSite())
        order_admin.message_user = MagicMock()
        order = Order.objects.get(id=trade.order_id)
        order_admin.taker_wins(HttpRequest(), Order.objects.filter(id=order.id))

        order = Order.objects.get(id=trade.order_id)
        self.assertEqual(order.status, Order.Status.MLD)
        self.assertGreater(order.taker.robot.earned_rewards, 0)
