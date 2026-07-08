from datetime import timedelta

from django.urls import reverse
from django.utils import timezone

from api.models import Order
from tests.pipeline.base import PipelineBaseTest
from tests.utils.trade import (
    Trade,
    maker_form_buy_with_range,
    maker_form_sell_with_range,
    read_file,
)


class CancelBuyTest(PipelineBaseTest):
    def test_cancel_buy_public_order(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.cancel_order()

        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 400)
        self.assertResponse(trade.response)

        self.assertEqual(data["error_code"], 1043)
        self.assertEqual(data["bad_request"], "This order has been cancelled")

        maker_headers = trade.get_robot_auth(trade.maker_index)
        maker_nick = read_file(f"tests/robots/{trade.maker_index}/nickname")
        response = self.client.get(reverse("notifications"), **maker_headers)
        self.assertResponse(response)
        notifications_data = list(response.json())
        self.assertEqual(notifications_data[0]["order_id"], trade.order_id)
        self.assertEqual(
            notifications_data[0]["title"],
            f"❌ Hey {maker_nick}, you have cancelled your public order with ID {trade.order_id}.",
        )

        trade.get_review()
        self.assertEqual(trade.response.status_code, 400)

    def test_cancel_buy_public_order_by_taker(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()

        trade.take_order()
        data = trade.response.json()
        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertTrue(data["is_taker"])

        trade.cancel_order(trade.taker_index)
        data = trade.response.json()
        self.assertEqual(trade.response.status_code, 200)
        self.assertFalse(data["is_participant"])
        self.assertFalse(data["is_taker"])
        self.assertFalse(data["is_maker"])
        self.assertTrue(
            (timezone.datetime.fromisoformat(data["penalty"]) - timezone.now())
            > timedelta(minutes=0)
        )
        self.assertTrue(
            (timezone.datetime.fromisoformat(data["penalty"]) - timezone.now())
            < timedelta(minutes=2)
        )

        trade.get_order(trade.maker_index)
        data = trade.response.json()
        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertTrue(data["is_maker"])

    def test_cancel_buy_public_order_by_third(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.take_order()

        data = trade.response.json()
        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertTrue(data["is_taker"])

        trade.take_order_third()
        data = trade.response.json()
        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertTrue(data["is_taker"])

        trade.cancel_order(trade.third_index)

        data = trade.response.json()
        self.assertFalse(data["is_participant"])
        self.assertFalse(data["is_taker"])
        self.assertFalse(data["is_maker"])
        self.assertTrue(
            (timezone.datetime.fromisoformat(data["penalty"]) - timezone.now())
            > timedelta(minutes=0)
        )
        self.assertTrue(
            (timezone.datetime.fromisoformat(data["penalty"]) - timezone.now())
            < timedelta(minutes=2)
        )

        trade.get_order(trade.maker_index)
        data = trade.response.json()
        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertTrue(data["is_maker"])

        trade.get_order(trade.taker_index)
        data = trade.response.json()
        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertTrue(data["is_participant"])
        self.assertTrue(data["is_taker"])

    def test_cancel_buy_pretaken_order_by_maker(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.take_order()
        trade.take_order_third()

        trade.cancel_order(trade.maker_index)
        data = trade.response.json()
        self.assertEqual(data["error_code"], 1043)
        self.assertEqual(data["bad_request"], "This order has been cancelled")

        trade.get_order(trade.taker_index)
        data = trade.response.json()
        self.assertEqual(data["error_code"], 1043)
        self.assertEqual(data["bad_request"], "This order has been cancelled")

        trade.get_order(trade.third_index)
        data = trade.response.json()
        self.assertEqual(data["error_code"], 1043)
        self.assertEqual(data["bad_request"], "This order has been cancelled")

    def test_cancel_buy_order_cancel_status(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)

        self.assertEqual(data["status_message"], Order.Status(Order.Status.PUB).label)

        trade.cancel_order(cancel_status=Order.Status.PUB)

        self.assertEqual(trade.response.status_code, 400)
        self.assertResponse(trade.response)

        data = trade.response.json()
        self.assertEqual(data["error_code"], 1043)
        self.assertEqual(data["bad_request"], "This order has been cancelled")

    def test_cancel_buy_order_different_cancel_status(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.pause_order()
        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)

        self.assertEqual(data["status_message"], Order.Status(Order.Status.PAU).label)

        trade.cancel_order(cancel_status=Order.Status.PUB)
        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 400)
        self.assertResponse(trade.response)

        data = trade.response.json()
        self.assertEqual(data["error_code"], 1020)
        self.assertEqual(
            data["bad_request"],
            f"Current order status is {Order.Status.PAU}, not {Order.Status.PUB}.",
        )

        trade.cancel_order()

    def test_collaborative_cancel_buy_order_in_chat(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.take_order()
        trade.take_order_third()
        trade.lock_taker_bond()
        trade.lock_escrow(trade.taker_index)
        trade.submit_payout_invoice(trade.maker_index)

        trade.cancel_order(trade.maker_index)
        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertTrue(trade.response.json()["asked_for_cancel"])

        trade.get_order(trade.taker_index)
        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertTrue(trade.response.json()["pending_cancel"])

        trade.cancel_order(trade.taker_index)
        self.assertEqual(trade.response.status_code, 400)
        self.assertResponse(trade.response)
        data = trade.response.json()
        self.assertEqual(data["error_code"], 1043)
        self.assertEqual(data["bad_request"], "This order has been cancelled")

        maker_headers = trade.get_robot_auth(trade.maker_index)
        maker_nick = read_file(f"tests/robots/{trade.maker_index}/nickname")
        response = self.client.get(reverse("notifications"), **maker_headers)
        self.assertResponse(response)
        notifications_data = list(response.json())
        self.assertEqual(notifications_data[0]["order_id"], trade.order_id)
        self.assertEqual(
            notifications_data[0]["title"],
            f"❌ Hey {maker_nick}, your order with ID {trade.order_id} has been collaboratively cancelled.",
        )
        taker_headers = trade.get_robot_auth(trade.taker_index)
        taker_nick = read_file(f"tests/robots/{trade.taker_index}/nickname")
        response = self.client.get(reverse("notifications"), **taker_headers)
        self.assertResponse(response)
        notifications_data = list(response.json())
        self.assertEqual(notifications_data[0]["order_id"], trade.order_id)
        self.assertEqual(
            notifications_data[0]["title"],
            f"❌ Hey {taker_nick}, your order with ID {trade.order_id} has been collaboratively cancelled.",
        )


class CancelSellTest(PipelineBaseTest):
    def test_cancel_sell_public_order(self):
        trade = Trade(self.client, maker_form=maker_form_sell_with_range)
        trade.publish_order()
        trade.cancel_order()

        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 400)
        self.assertResponse(trade.response)

        self.assertEqual(data["error_code"], 1043)
        self.assertEqual(data["bad_request"], "This order has been cancelled")

        maker_headers = trade.get_robot_auth(trade.maker_index)
        maker_nick = read_file(f"tests/robots/{trade.maker_index}/nickname")
        response = self.client.get(reverse("notifications"), **maker_headers)
        self.assertResponse(response)
        notifications_data = list(response.json())
        self.assertEqual(notifications_data[0]["order_id"], trade.order_id)
        self.assertEqual(
            notifications_data[0]["title"],
            f"❌ Hey {maker_nick}, you have cancelled your public order with ID {trade.order_id}.",
        )

        trade.get_review()
        self.assertEqual(trade.response.status_code, 400)

    def test_cancel_sell_public_order_by_taker(self):
        trade = Trade(self.client, maker_form=maker_form_sell_with_range)
        trade.publish_order()

        trade.take_order()
        data = trade.response.json()
        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertTrue(data["is_taker"])

        trade.cancel_order(trade.taker_index)
        data = trade.response.json()
        self.assertEqual(trade.response.status_code, 200)
        self.assertFalse(data["is_participant"])
        self.assertFalse(data["is_taker"])
        self.assertFalse(data["is_maker"])
        self.assertTrue(
            (timezone.datetime.fromisoformat(data["penalty"]) - timezone.now())
            > timedelta(minutes=0)
        )
        self.assertTrue(
            (timezone.datetime.fromisoformat(data["penalty"]) - timezone.now())
            < timedelta(minutes=2)
        )

        trade.get_order(trade.maker_index)
        data = trade.response.json()
        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertTrue(data["is_maker"])

    def test_cancel_sell_public_order_by_third(self):
        trade = Trade(self.client, maker_form=maker_form_sell_with_range)
        trade.publish_order()
        trade.take_order()

        data = trade.response.json()
        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertTrue(data["is_taker"])

        trade.take_order_third()
        data = trade.response.json()
        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertTrue(data["is_taker"])

        trade.cancel_order(trade.third_index)

        data = trade.response.json()
        self.assertFalse(data["is_participant"])
        self.assertFalse(data["is_taker"])
        self.assertFalse(data["is_maker"])
        self.assertTrue(
            (timezone.datetime.fromisoformat(data["penalty"]) - timezone.now())
            > timedelta(minutes=0)
        )
        self.assertTrue(
            (timezone.datetime.fromisoformat(data["penalty"]) - timezone.now())
            < timedelta(minutes=2)
        )

        trade.get_order(trade.maker_index)
        data = trade.response.json()
        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertTrue(data["is_maker"])

        trade.get_order(trade.taker_index)
        data = trade.response.json()
        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertTrue(data["is_participant"])
        self.assertTrue(data["is_taker"])

    def test_cancel_sell_pretaken_order_by_maker(self):
        trade = Trade(self.client, maker_form=maker_form_sell_with_range)
        trade.publish_order()
        trade.take_order()
        trade.take_order_third()

        trade.cancel_order(trade.maker_index)
        data = trade.response.json()
        self.assertEqual(data["error_code"], 1043)
        self.assertEqual(data["bad_request"], "This order has been cancelled")

        trade.get_order(trade.taker_index)
        data = trade.response.json()
        self.assertEqual(data["error_code"], 1043)
        self.assertEqual(data["bad_request"], "This order has been cancelled")

        trade.get_order(trade.third_index)
        data = trade.response.json()
        self.assertEqual(data["error_code"], 1043)
        self.assertEqual(data["bad_request"], "This order has been cancelled")

    def test_cancel_sell_order_cancel_status(self):
        trade = Trade(self.client, maker_form=maker_form_sell_with_range)
        trade.publish_order()
        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)

        self.assertEqual(data["status_message"], Order.Status(Order.Status.PUB).label)

        trade.cancel_order(cancel_status=Order.Status.PUB)

        self.assertEqual(trade.response.status_code, 400)
        self.assertResponse(trade.response)

        data = trade.response.json()
        self.assertEqual(data["error_code"], 1043)
        self.assertEqual(data["bad_request"], "This order has been cancelled")

    def test_cancel_sell_order_different_cancel_status(self):
        trade = Trade(self.client, maker_form=maker_form_sell_with_range)
        trade.publish_order()
        trade.pause_order()
        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)

        self.assertEqual(data["status_message"], Order.Status(Order.Status.PAU).label)

        trade.cancel_order(cancel_status=Order.Status.PUB)
        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 400)
        self.assertResponse(trade.response)

        data = trade.response.json()
        self.assertEqual(data["error_code"], 1020)
        self.assertEqual(
            data["bad_request"],
            f"Current order status is {Order.Status.PAU}, not {Order.Status.PUB}.",
        )

        trade.cancel_order()

    def test_collaborative_cancel_sell_order_in_chat(self):
        trade = Trade(self.client, maker_form=maker_form_sell_with_range)
        trade.publish_order()
        trade.take_order()
        trade.take_order_third()
        trade.lock_taker_bond()
        trade.lock_escrow(trade.maker_index)
        trade.submit_payout_invoice(trade.taker_index)

        trade.cancel_order(trade.maker_index)
        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertTrue(trade.response.json()["asked_for_cancel"])

        trade.get_order(trade.taker_index)
        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertTrue(trade.response.json()["pending_cancel"])

        trade.cancel_order(trade.taker_index)
        self.assertEqual(trade.response.status_code, 400)
        self.assertResponse(trade.response)
        data = trade.response.json()
        self.assertEqual(data["error_code"], 1043)
        self.assertEqual(data["bad_request"], "This order has been cancelled")

        maker_headers = trade.get_robot_auth(trade.maker_index)
        maker_nick = read_file(f"tests/robots/{trade.maker_index}/nickname")
        response = self.client.get(reverse("notifications"), **maker_headers)
        self.assertResponse(response)
        notifications_data = list(response.json())
        self.assertEqual(notifications_data[0]["order_id"], trade.order_id)
        self.assertEqual(
            notifications_data[0]["title"],
            f"❌ Hey {maker_nick}, your order with ID {trade.order_id} has been collaboratively cancelled.",
        )
        taker_headers = trade.get_robot_auth(trade.taker_index)
        taker_nick = read_file(f"tests/robots/{trade.taker_index}/nickname")
        response = self.client.get(reverse("notifications"), **taker_headers)
        self.assertResponse(response)
        notifications_data = list(response.json())
        self.assertEqual(notifications_data[0]["order_id"], trade.order_id)
        self.assertEqual(
            notifications_data[0]["title"],
            f"❌ Hey {taker_nick}, your order with ID {trade.order_id} has been collaboratively cancelled.",
        )
