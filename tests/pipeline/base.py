from decouple import config
from django.contrib.admin.sites import AdminSite
from django.contrib.auth.models import User

from api.admin import OrderAdmin
from api.models import Order
from api.tasks import cache_market
from control.tasks import compute_node_balance
from tests.test_api import BaseAPITestCase
from tests.utils.node import set_up_regtest_network
from tests.utils.trade import read_file


class PipelineBaseTest(BaseAPITestCase):
    su_pass = "12345678"
    su_name = config("ESCROW_USERNAME", cast=str, default="admin")

    @classmethod
    def setUpTestData(cls):
        User.objects.create_superuser(cls.su_name, "super@user.com", cls.su_pass)
        cache_market()
        set_up_regtest_network()
        compute_node_balance()

    def assert_order_logs(self, order_id):
        order = Order.objects.get(id=order_id)
        order_admin = OrderAdmin(model=Order, admin_site=AdminSite())
        try:
            result = order_admin._logs(order)
            self.assertIsInstance(result, str)
        except Exception as e:
            self.fail(f"Exception occurred: {e}")

    def assert_robot(self, response, robot_index):
        nickname = read_file(f"tests/robots/{robot_index}/nickname")
        pub_key = read_file(f"tests/robots/{robot_index}/pub_key")
        enc_priv_key = read_file(f"tests/robots/{robot_index}/enc_priv_key")

        data = response.json()

        self.assertEqual(response.status_code, 200)
        self.assertResponse(response)

        self.assertEqual(
            data["nickname"],
            nickname,
            f"Robot created nickname is not {nickname}",
        )
        self.assertEqual(
            data["public_key"], pub_key, "Returned public key does not match"
        )
        self.assertEqual(
            data["encrypted_private_key"],
            enc_priv_key,
            "Returned encrypted private key does not match",
        )
        self.assertEqual(
            len(data["tg_token"]), 15, "String is not exactly 15 characters long"
        )
        self.assertEqual(
            data["tg_bot_name"],
            config(
                "TELEGRAM_BOT_NAME", cast=str, default="RoboCoordinatorNotificationBot"
            ),
            "Telegram bot name is not correct",
        )
        self.assertFalse(
            data["tg_enabled"], "The new robot's telegram seems to be enabled"
        )
        self.assertEqual(data["earned_rewards"], 0, "The new robot's rewards are not 0")
