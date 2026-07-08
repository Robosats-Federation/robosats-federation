from datetime import timedelta


from api.models import Order
from django.utils import timezone
from tests.pipeline.base import PipelineBaseTest
from tests.utils.trade import Trade, maker_form_buy_with_range, read_file


class BuyTakeLockTest(PipelineBaseTest):
    def test_make_and_take_order(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.take_order()

        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)

        self.assertEqual(data["status_message"], Order.Status(Order.Status.PUB).label)
        self.assertEqual(
            data["ur_nick"], read_file(f"tests/robots/{trade.taker_index}/nickname")
        )
        self.assertEqual(data["taker_nick"], "None")
        self.assertEqual(
            data["maker_nick"], read_file(f"tests/robots/{trade.maker_index}/nickname")
        )
        self.assertIsHash(data["maker_hash_id"])
        self.assertEqual(data["maker_status"], "Active")
        self.assertAlmostEqual(float(data["amount"]), 100)
        self.assertFalse(data["is_maker"])
        self.assertFalse(data["is_buyer"])
        self.assertTrue(data["is_seller"])
        self.assertTrue(data["is_taker"])
        self.assertTrue(data["is_participant"])
        self.assertTrue(data["maker_locked"])
        self.assertFalse(data["taker_locked"])
        self.assertFalse(data["escrow_locked"])
        self.assertTrue(
            (timezone.datetime.fromisoformat(data["expires_at"]) - timezone.now())
            > timedelta(minutes=2)
        )
        self.assertTrue(
            (timezone.datetime.fromisoformat(data["expires_at"]) - timezone.now())
            < timedelta(minutes=4)
        )

        trade.cancel_order()
        self.assert_order_logs(data["id"])

    def test_make_and_take_range_order(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.take_order()
        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertEqual(data["status_message"], Order.Status(Order.Status.PUB).label)
        self.assertAlmostEqual(float(data["amount"]), 100)

        trade.cancel_order()

    def test_make_and_take_description_order(self):
        description = "Test"
        description_maker_form = maker_form_buy_with_range.copy()
        description_maker_form["description"] = description

        trade = Trade(self.client, maker_form=description_maker_form)
        trade.publish_order()
        data = trade.response.json()
        self.assertEqual(trade.response.status_code, 200)
        self.assertEqual(data["description"], description)

        trade.take_order()
        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertEqual(data["status_message"], Order.Status(Order.Status.PUB).label)
        self.assertEqual(data["description"], description)

        trade.cancel_order()

    def test_make_and_take_password_order(self):
        password = "1234567"
        password_maker_form = maker_form_buy_with_range.copy()
        password_maker_form["password"] = password

        trade = Trade(self.client, maker_form=password_maker_form)
        trade.publish_order()

        data = trade.response.json()
        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)

        trade.get_order(trade.maker_index)
        data = trade.response.json()
        self.assertEqual(trade.response.status_code, 200)

        trade.get_order(trade.taker_index)
        data = trade.response.json()
        self.assertEqual(trade.response.status_code, 200)
        self.assertTrue(data["has_password"])
        self.assertIsInstance(data["satoshis_now"], int)
        self.assertNotIn("is_buyer", data)

        trade.take_order()
        data = trade.response.json()
        self.assertEqual(trade.response.status_code, 403)
        self.assertEqual(data["error_code"], 1045)
        self.assertEqual(data["bad_request"], "Wrong password")

        trade.take_password_order("test")
        data = trade.response.json()
        self.assertEqual(trade.response.status_code, 403)
        self.assertEqual(data["error_code"], 1045)
        self.assertEqual(data["bad_request"], "Wrong password")

        trade.take_password_order(password)
        data = trade.response.json()
        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertEqual(data["status_message"], Order.Status(Order.Status.PUB).label)
        self.assertAlmostEqual(float(data["amount"]), 100)

        trade.cancel_order()

    def test_make_and_take_order_multiple_takers(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()

        trade.take_order_third()
        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertEqual(data["status_message"], Order.Status(Order.Status.PUB).label)
        self.assertEqual(
            data["ur_nick"], read_file(f"tests/robots/{trade.third_index}/nickname")
        )
        self.assertEqual(data["taker_nick"], "None")
        self.assertEqual(
            data["maker_nick"], read_file(f"tests/robots/{trade.maker_index}/nickname")
        )
        self.assertIsHash(data["maker_hash_id"])
        self.assertEqual(data["maker_status"], "Active")
        self.assertFalse(data["is_maker"])
        self.assertFalse(data["is_buyer"])
        self.assertTrue(data["is_seller"])
        self.assertTrue(data["is_taker"])
        self.assertTrue(data["is_participant"])
        self.assertTrue(data["maker_locked"])
        self.assertFalse(data["taker_locked"])
        self.assertFalse(data["escrow_locked"])
        self.assertTrue(
            (timezone.datetime.fromisoformat(data["expires_at"]) - timezone.now())
            > timedelta(minutes=2)
        )
        self.assertTrue(
            (timezone.datetime.fromisoformat(data["expires_at"]) - timezone.now())
            < timedelta(minutes=4)
        )

        third_invoice = data["bond_invoice"]

        trade.take_order()
        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertNotEqual(third_invoice, data["bond_invoice"])
        self.assertEqual(data["status_message"], Order.Status(Order.Status.PUB).label)
        self.assertEqual(
            data["ur_nick"], read_file(f"tests/robots/{trade.taker_index}/nickname")
        )
        self.assertEqual(data["taker_nick"], "None")
        self.assertEqual(
            data["maker_nick"], read_file(f"tests/robots/{trade.maker_index}/nickname")
        )
        self.assertIsHash(data["maker_hash_id"])
        self.assertEqual(data["maker_status"], "Active")
        self.assertFalse(data["is_maker"])
        self.assertFalse(data["is_buyer"])
        self.assertTrue(data["is_seller"])
        self.assertTrue(data["is_taker"])
        self.assertTrue(data["is_participant"])
        self.assertTrue(data["maker_locked"])
        self.assertFalse(data["taker_locked"])
        self.assertFalse(data["escrow_locked"])
        self.assertTrue(
            (timezone.datetime.fromisoformat(data["expires_at"]) - timezone.now())
            > timedelta(minutes=2)
        )
        self.assertTrue(
            (timezone.datetime.fromisoformat(data["expires_at"]) - timezone.now())
            < timedelta(minutes=4)
        )

        trade.cancel_order()
        self.assert_order_logs(data["id"])

    def test_make_and_lock_contract(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.take_order()
        trade.lock_taker_bond()

        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertEqual(data["status_message"], Order.Status(Order.Status.WF2).label)
        self.assertEqual(data["maker_status"], "Active")
        self.assertEqual(data["taker_status"], "Active")
        self.assertTrue(data["is_participant"])
        self.assertTrue(data["maker_locked"])
        self.assertTrue(data["taker_locked"])
        self.assertFalse(data["escrow_locked"])
        self.assertTrue(
            (timezone.datetime.fromisoformat(data["expires_at"]) - timezone.now())
            > timedelta(minutes=140)
        )
        self.assertTrue(
            (timezone.datetime.fromisoformat(data["expires_at"]) - timezone.now())
            < timedelta(minutes=150)
        )

        trade.get_order(trade.maker_index)
        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertEqual(data["status_message"], Order.Status(Order.Status.WF2).label)
        self.assertTrue(data["swap_allowed"])
        self.assertIsInstance(data["suggested_mining_fee_rate"], float)
        self.assertIsInstance(data["swap_fee_rate"], float)
        self.assertTrue(data["suggested_mining_fee_rate"] > 0)
        self.assertTrue(data["swap_fee_rate"] > 0)
        self.assertEqual(data["maker_status"], "Active")
        self.assertEqual(data["taker_status"], "Active")
        self.assertTrue(data["is_participant"])
        self.assertTrue(data["maker_locked"])
        self.assertTrue(data["taker_locked"])
        self.assertFalse(data["escrow_locked"])
        self.assertTrue(
            (timezone.datetime.fromisoformat(data["expires_at"]) - timezone.now())
            > timedelta(minutes=140)
        )
        self.assertTrue(
            (timezone.datetime.fromisoformat(data["expires_at"]) - timezone.now())
            < timedelta(minutes=150)
        )

        trade.cancel_order()
        self.assert_order_logs(data["id"])

    def test_make_and_lock_contract_multiple_takers(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.take_order()
        trade.take_order_third()

        third_invoice = trade.response.json()["bond_invoice"]
        trade.get_order(trade.taker_index)
        taker_invoice = trade.response.json()["bond_invoice"]
        trade.pay_invoice(taker_invoice)
        trade.pay_invoice(third_invoice)
        trade.follow_hold_invoices()
        trade.get_order(trade.taker_index)

        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertEqual(data["status_message"], Order.Status(Order.Status.WF2).label)
        self.assertEqual(data["maker_status"], "Active")
        self.assertEqual(data["taker_status"], "Active")
        self.assertTrue(data["is_participant"])
        self.assertTrue(data["maker_locked"])
        self.assertTrue(data["taker_locked"])
        self.assertFalse(data["escrow_locked"])
        self.assertTrue(
            (timezone.datetime.fromisoformat(data["expires_at"]) - timezone.now())
            > timedelta(minutes=140)
        )
        self.assertTrue(
            (timezone.datetime.fromisoformat(data["expires_at"]) - timezone.now())
            < timedelta(minutes=150)
        )
        self.assert_order_logs(data["id"])

        trade.get_order(trade.maker_index)
        data = trade.response.json()
        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertEqual(data["status_message"], Order.Status(Order.Status.WF2).label)
        self.assertTrue(data["swap_allowed"])
        self.assertIsInstance(data["suggested_mining_fee_rate"], float)
        self.assertIsInstance(data["swap_fee_rate"], float)
        self.assertTrue(data["suggested_mining_fee_rate"] > 0)
        self.assertTrue(data["swap_fee_rate"] > 0)
        self.assertEqual(data["maker_status"], "Active")
        self.assertEqual(data["taker_status"], "Active")
        self.assertTrue(data["is_participant"])
        self.assertTrue(data["maker_locked"])
        self.assertTrue(data["taker_locked"])
        self.assertFalse(data["escrow_locked"])
        self.assertTrue(
            (timezone.datetime.fromisoformat(data["expires_at"]) - timezone.now())
            > timedelta(minutes=140)
        )
        self.assertTrue(
            (timezone.datetime.fromisoformat(data["expires_at"]) - timezone.now())
            < timedelta(minutes=150)
        )
        self.assert_order_logs(data["id"])

        trade.get_order(trade.third_index)
        data = trade.response.json()
        self.assertEqual(trade.response.status_code, 403)
        self.assertEqual(data["error_code"], 1044)
        self.assertEqual(data["bad_request"], "This order is not available")

        trade.cancel_order()

    def test_trade_to_locked_escrow(self):
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.take_order()
        trade.take_order_third()
        trade.lock_taker_bond()
        trade.lock_escrow(trade.taker_index)

        data = trade.response.json()

        self.assertEqual(trade.response.status_code, 200)
        self.assertResponse(trade.response)
        self.assertEqual(data["status_message"], Order.Status(Order.Status.WFI).label)
        self.assertTrue(data["maker_locked"])
        self.assertTrue(data["taker_locked"])
        self.assertTrue(data["escrow_locked"])

        trade.cancel_order(trade.taker_index)
