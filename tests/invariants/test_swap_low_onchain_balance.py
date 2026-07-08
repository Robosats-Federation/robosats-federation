from unittest.mock import patch

from tests.pipeline.base import PipelineBaseTest
from tests.utils.trade import Trade, maker_form_buy_with_range


class TestSwapLowOnchainBalance(PipelineBaseTest):
    @patch("api.logics.Logics.create_onchain_payment", return_value=False)
    def test_swap_denied_when_coordinator_balance_low(self, mock_create):
        """
        Simulates insufficient coordinator onchain balance.
        The buyer fetches the order at WFI and receives swap_allowed=False
        with an explanatory swap_failure_reason.
        """
        trade = Trade(self.client, maker_form=maker_form_buy_with_range)
        trade.publish_order()
        trade.take_order()
        trade.take_order_third()
        trade.lock_taker_bond()
        trade.lock_escrow(trade.taker_index)
        # lock_escrow already leaves the buyer (maker) response in trade.response at WFI,
        # since it internally calls get_order() (default maker_index) after escrow is locked.

        data = trade.response.json()

        self.assertFalse(data["swap_allowed"])
        self.assertIn(
            "Not enough onchain liquidity available to offer a swap",
            data["swap_failure_reason"],
        )
