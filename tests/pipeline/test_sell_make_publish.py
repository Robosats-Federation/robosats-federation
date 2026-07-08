from datetime import datetime, timedelta

from django.urls import reverse

from api.models import Order
from django.utils import timezone
from tests.pipeline.base import PipelineBaseTest
from tests.utils.trade import Trade, maker_form_sell_with_range, read_file


class SellMakePublishTest(PipelineBaseTest):
    def test_sell_make_order(self):
        trade = Trade(self.client, maker_form=maker_form_sell_with_range)
        data = trade.response.json()

        self.assertResponse(trade.response)
        self.assertIsInstance(data["id"], int)
        self.assertEqual(data["status"], Order.Status.WFB)
        self.assertIsInstance(datetime.fromisoformat(data["created_at"]), datetime)
        self.assertIsInstance(datetime.fromisoformat(data["expires_at"]), datetime)
        self.assertEqual(data["type"], Order.Types.SELL)
        self.assertEqual(data["currency"], 1)
        self.assertIsNone(data["amount"])
        self.assertTrue(data["has_range"])
        self.assertAlmostEqual(
            float(data["min_amount"]), trade.maker_form["min_amount"]
        )
        self.assertAlmostEqual(
            float(data["max_amount"]), trade.maker_form["max_amount"]
        )
        self.assertEqual(data["payment_method"], trade.maker_form["payment_method"])
        self.assertEqual(data["escrow_duration"], trade.maker_form["escrow_duration"])
        self.assertAlmostEqual(float(data["bond_size"]), trade.maker_form["bond_size"])
        self.assertAlmostEqual(float(data["latitude"]), trade.maker_form["latitude"])
        self.assertAlmostEqual(float(data["longitude"]), trade.maker_form["longitude"])
        self.assertAlmostEqual(float(data["premium"]), trade.maker_form["premium"])
        self.assertFalse(data["is_explicit"])
        self.assertIsNone(data["satoshis"])
        self.assertIsNone(data["taker"])
        self.assert_order_logs(data["id"])

    def test_sell_make_order_on_blocked_country(self):
        trade = Trade(
            self.client,
            maker_form={
                "type": Order.Types.SELL,
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

    def test_sell_get_order_created(self):
        trade = Trade(self.client, maker_form=maker_form_sell_with_range)

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
        self.assertFalse(data["is_buyer"])
        self.assertTrue(data["is_seller"])
        self.assertEqual(data["maker_status"], "Active")
        self.assertEqual(data["status_message"], Order.Status(Order.Status.WFB).label)
        self.assertFalse(data["is_fiat_sent"])
        self.assertFalse(data["is_disputed"])
        self.assertEqual(
            data["ur_nick"], read_file(f"tests/robots/{trade.maker_index}/nickname")
        )
        self.assertEqual(
            data["maker_nick"], read_file(f"tests/robots/{trade.maker_index}/nickname")
        )
        self.assertIsHash(data["maker_hash_id"])
        self.assertIsInstance(data["satoshis_now"], int)
        self.assertFalse(data["maker_locked"])
        self.assertFalse(data["taker_locked"])
        self.assertFalse(data["escrow_locked"])
        self.assertIsInstance(data["bond_satoshis"], int)

        trade.cancel_order()
        self.assert_order_logs(data["id"])

    def test_sell_publish_order(self):
        trade = Trade(self.client, maker_form=maker_form_sell_with_range)
        trade.publish_order()
        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
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

    def test_sell_pause_unpause_order(self):
        trade = Trade(self.client, maker_form=maker_form_sell_with_range)
        trade.publish_order()

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
