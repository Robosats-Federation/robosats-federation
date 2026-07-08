from datetime import datetime

from django.urls import reverse

from tests.pipeline.base import PipelineBaseTest
from tests.utils.trade import Trade, maker_form_buy_with_range


class TestBook(PipelineBaseTest):
    def test_book_with_order(self):
        path = reverse("book")

        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()

        response = self.client.get(path)
        data = response.json()

        self.assertEqual(response.status_code, 200)
        self.assertResponse(response)

        self.assertIsInstance(datetime.fromisoformat(data[0]["created_at"]), datetime)
        self.assertIsInstance(datetime.fromisoformat(data[0]["expires_at"]), datetime)
        self.assertIsHash(data[0]["maker_hash_id"])
        self.assertIsNone(data[0]["amount"])
        self.assertAlmostEqual(
            float(data[0]["min_amount"]), trade.maker_form["min_amount"]
        )
        self.assertAlmostEqual(
            float(data[0]["max_amount"]), trade.maker_form["max_amount"]
        )
        self.assertAlmostEqual(float(data[0]["latitude"]), trade.maker_form["latitude"])
        self.assertAlmostEqual(
            float(data[0]["longitude"]), trade.maker_form["longitude"]
        )
        self.assertEqual(
            data[0]["escrow_duration"], trade.maker_form["escrow_duration"]
        )
        self.assertFalse(data[0]["is_explicit"])

        trade.cancel_order()
