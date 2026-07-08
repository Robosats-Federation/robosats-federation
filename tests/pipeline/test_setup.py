from datetime import datetime
from decimal import Decimal

from django.urls import reverse

from api.models import Currency
from control.models import BalanceLog
from tests.pipeline.base import PipelineBaseTest
from tests.utils.trade import Trade


class SetupTest(PipelineBaseTest):
    def test_login_superuser(self):
        path = reverse("admin:login")
        data = {"username": self.su_name, "password": self.su_pass}
        response = self.client.post(path, data)
        self.assertEqual(response.status_code, 302)
        self.assertResponse(response)

    def test_cache_market(self):
        usd = Currency.objects.get(id=1)
        self.assertIsInstance(
            usd.exchange_rate,
            Decimal,
            f"Exchange rate is not a Decimal. Got {type(usd.exchange_rate)}",
        )
        self.assertGreater(
            usd.exchange_rate, 0, "Exchange rate is not higher than zero"
        )
        self.assertIsInstance(
            usd.timestamp, datetime, "External price timestamp is not a datetime"
        )

    def test_initial_balance_log(self):
        balance_log = BalanceLog.objects.latest()
        self.assertIsInstance(balance_log.time, datetime)
        self.assertTrue(balance_log.total > 0)
        self.assertTrue(balance_log.ln_local > 0)
        self.assertTrue(balance_log.ln_local_unsettled >= 0)
        self.assertTrue(balance_log.ln_remote > 0)
        self.assertEqual(balance_log.ln_remote_unsettled, 0)
        self.assertTrue(balance_log.onchain_total > 0)
        self.assertTrue(balance_log.onchain_confirmed > 0)
        self.assertEqual(balance_log.onchain_unconfirmed, 0)
        self.assertTrue(balance_log.onchain_fraction > 0)

    def test_create_robots(self):
        trade = Trade(self.client)
        for robot_index in [1, 2]:
            response = trade.create_robot(robot_index)
            self.assert_robot(response, robot_index)
