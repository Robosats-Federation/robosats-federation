from datetime import datetime, timedelta

from django.urls import reverse

from api.models import Order
from django.utils import timezone
from tests.pipeline.base import PipelineBaseTest
from tests.utils.trade import Trade, maker_form_buy_with_range, read_file


class BuyMakePublishTest(PipelineBaseTest):
    def test_make_order(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        data = trade.response.json()

        self.assertResponse(trade.response)
        self.assertIsInstance(data["id"], int, "Order ID is not an integer")
        self.assertEqual(
            data["status"],
            Order.Status.WFB,
            "Newly created order status is not 'Waiting for maker bond'",
        )
        self.assertIsInstance(
            datetime.fromisoformat(data["created_at"]),
            datetime,
            "Order creation timestamp is not datetime",
        )
        self.assertIsInstance(
            datetime.fromisoformat(data["expires_at"]),
            datetime,
            "Order expiry time is not datetime",
        )
        self.assertEqual(
            data["type"], Order.Types.BUY, "Buy order is not of type value BUY"
        )
        self.assertEqual(data["currency"], 1, "Order for USD is not of currency USD")
        self.assertIsNone(
            data["amount"], "Order with range has a non-null simple amount"
        )
        self.assertTrue(data["has_range"], "Order with range has a False has_range")
        self.assertAlmostEqual(
            float(data["min_amount"]),
            trade.maker_form["min_amount"],
            "Order min amount does not match",
        )
        self.assertAlmostEqual(
            float(data["max_amount"]),
            trade.maker_form["max_amount"],
            "Order max amount does not match",
        )
        self.assertEqual(
            data["payment_method"],
            trade.maker_form["payment_method"],
            "Order payment method does not match",
        )
        self.assertEqual(
            data["escrow_duration"],
            trade.maker_form["escrow_duration"],
            "Order escrow duration does not match",
        )
        self.assertAlmostEqual(
            float(data["bond_size"]),
            trade.maker_form["bond_size"],
            "Order bond size does not match",
        )
        self.assertAlmostEqual(
            float(data["latitude"]),
            trade.maker_form["latitude"],
            "Order latitude does not match",
        )
        self.assertAlmostEqual(
            float(data["longitude"]),
            trade.maker_form["longitude"],
            "Order longitude does not match",
        )
        self.assertAlmostEqual(
            float(data["premium"]),
            trade.maker_form["premium"],
            "Order premium does not match",
        )
        self.assertFalse(
            data["is_explicit"], "Relative pricing order has True is_explicit"
        )
        self.assertIsNone(
            data["satoshis"], "Relative pricing order has non-null Satoshis"
        )
        self.assertIsNone(data["taker"], "New order's taker is not null")
        self.assert_order_logs(data["id"])

        maker_headers = trade.get_robot_auth(trade.maker_index)
        response = self.client.get(reverse("notifications"), **maker_headers)
        self.assertResponse(response)
        notifications_data = list(response.json())
        self.assertEqual(
            len(notifications_data),
            0,
            "User has no notification",
        )

    def test_make_order_on_blocked_country(self):
        trade = Trade(
            self.client,
            maker_form={
                "type": 0,
                "currency": 1,
                "has_range": True,
                "min_amount": 84,
                "max_amount": 201.7,
                "payment_method": "Advcash Cash F2F",
                "is_explicit": False,
                "premium": 3.34,
                "public_duration": 69360,
                "escrow_duration": 8700,
                "bond_size": 3.5,
                "latitude": -11.8014,
                "longitude": 17.3575,
            },
        )
        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 400)
        self.assertResponse(trade.response)
        self.assertEqual(data["error_code"], 1010)
        self.assertEqual(
            data["bad_request"], "The coordinator does not support orders in AGO"
        )

    def test_get_order_created(self):
        robot_index = 1
        trade = Trade(
            self.client, maker_form=maker_form_buy_with_range, maker_index=robot_index
        )

        trade.get_order()
        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertEqual(data["id"], trade.order_id)
        self.assertIsInstance(datetime.fromisoformat(data["created_at"]), datetime)
        self.assertIsInstance(datetime.fromisoformat(data["expires_at"]), datetime)
        self.assertTrue(
            (timezone.datetime.fromisoformat(data["expires_at"]) - timezone.now())
            > timedelta(minutes=2)
        )
        self.assertTrue(
            (timezone.datetime.fromisoformat(data["expires_at"]) - timezone.now())
            < timedelta(minutes=5)
        )
        self.assertTrue(data["is_maker"])
        self.assertTrue(data["is_participant"])
        self.assertTrue(data["is_buyer"])
        self.assertFalse(data["is_seller"])
        self.assertEqual(data["maker_status"], "Active")
        self.assertEqual(data["status_message"], Order.Status(Order.Status.WFB).label)
        self.assertFalse(data["is_fiat_sent"])
        self.assertFalse(data["is_disputed"])
        self.assertEqual(
            data["ur_nick"], read_file(f"tests/robots/{robot_index}/nickname")
        )
        self.assertEqual(
            data["maker_nick"], read_file(f"tests/robots/{robot_index}/nickname")
        )
        self.assertIsHash(data["maker_hash_id"])
        self.assertIsInstance(data["satoshis_now"], int)
        self.assertFalse(data["maker_locked"])
        self.assertFalse(data["taker_locked"])
        self.assertFalse(data["escrow_locked"])
        self.assertIsInstance(data["bond_satoshis"], int)

        trade.cancel_order()
        self.assert_order_logs(data["id"])

    def test_publish_order(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertEqual(data["id"], data["id"])
        self.assertEqual(data["status_message"], Order.Status(Order.Status.PUB).label)
        self.assertTrue(data["maker_locked"])
        self.assertFalse(data["taker_locked"])
        self.assertFalse(data["escrow_locked"])
        self.assertTrue(
            (timezone.datetime.fromisoformat(data["expires_at"]) - timezone.now())
            > timedelta(minutes=1150)
        )
        self.assertTrue(
            (timezone.datetime.fromisoformat(data["expires_at"]) - timezone.now())
            < timedelta(minutes=1160)
        )

        trade.get_order(robot_index=2)
        public_data = trade.response.json()
        self.assertFalse(public_data["is_participant"])
        self.assertIsInstance(public_data["price_now"], float)
        self.assertIsInstance(data["satoshis_now"], int)

        maker_headers = trade.get_robot_auth(trade.maker_index)
        response = self.client.get(reverse("notifications"), **maker_headers)
        self.assertResponse(response)
        notifications_data = list(response.json())
        self.assertEqual(notifications_data[0]["order_id"], trade.order_id)
        self.assertEqual(
            notifications_data[0]["title"],
            f"✅ Hey {data['maker_nick']}, your order with ID {trade.order_id} is public in the order book.",
        )

        trade.cancel_order()
        self.assert_order_logs(data["id"])

    def test_pause_unpause_order(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        data = trade.response.json()

        trade.pause_order()
        data = trade.response.json()
        self.assertResponse(trade.response)
        self.assertEqual(data["status_message"], Order.Status(Order.Status.PAU).label)

        trade.pause_order()
        data = trade.response.json()
        self.assertResponse(trade.response)
        self.assertEqual(data["status_message"], Order.Status(Order.Status.PUB).label)

        trade.cancel_order()
        self.assert_order_logs(data["id"])
