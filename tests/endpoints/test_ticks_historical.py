from datetime import datetime

from django.urls import reverse

from control.tasks import do_accounting
from tests.pipeline.base import PipelineBaseTest
from tests.utils.trade import Trade, maker_form_buy_with_range


class TestTicksHistorical(PipelineBaseTest):
    def test_ticks(self):
        path = reverse("ticks")
        params = "?start=01-01-1970&end=01-01-2070"

        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.take_order()
        trade.take_order_third()
        trade.lock_taker_bond()
        trade.cancel_order()

        response = self.client.get(path + params)
        data = response.json()

        self.assertEqual(response.status_code, 200)
        self.assertResponse(response)

        self.assertIsInstance(datetime.fromisoformat(data[0]["timestamp"]), datetime)
        self.assertIsInstance(data[0]["volume"], str)
        self.assertIsInstance(data[0]["price"], str)
        self.assertIsInstance(data[0]["premium"], str)
        self.assertIsInstance(data[0]["fee"], str)

    def test_daily_historical(self):
        path = reverse("historical")

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

        do_accounting()

        response = self.client.get(path)
        data = response.json()

        self.assertEqual(response.status_code, 200)
        first_date = list(data.keys())[0]
        self.assertIsInstance(datetime.fromisoformat(first_date), datetime)
        self.assertIsInstance(data[first_date]["volume"], float)
        self.assertIsInstance(data[first_date]["num_contracts"], int)
