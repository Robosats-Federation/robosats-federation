import hashlib

from datetime import timedelta

import requests
from decouple import config
from django.utils import timezone


PAYOUT_POLICY_ENABLED = config("PAYOUT_POLICY_ENABLED", cast=bool, default=False)
PAYOUT_POLICY_URL = config("PAYOUT_POLICY_URL", default="")
PAYOUT_POLICY_ONCHAIN_ENABLED = config(
    "PAYOUT_POLICY_ONCHAIN_ENABLED", cast=bool, default=False
)
PAYOUT_POLICY_ONCHAIN_URL = config("PAYOUT_POLICY_ONCHAIN_URL", default="")
PAYOUT_POLICY_KEYSEND_ENABLED = config(
    "PAYOUT_POLICY_KEYSEND_ENABLED", cast=bool, default=False
)
PAYOUT_POLICY_KEYSEND_URL = config("PAYOUT_POLICY_KEYSEND_URL", default="")
PAYOUT_POLICY_TOKEN = config("PAYOUT_POLICY_TOKEN", default="")
PAYOUT_POLICY_TIMEOUT_SECONDS = config(
    "PAYOUT_POLICY_TIMEOUT_SECONDS", cast=int, default=170
)
PAYOUT_POLICY_CONNECT_TIMEOUT_SECONDS = config(
    "PAYOUT_POLICY_CONNECT_TIMEOUT_SECONDS", cast=float, default=2.0
)
PAYOUT_POLICY_KEYSEND_TIMEOUT_SECONDS = config(
    "PAYOUT_POLICY_KEYSEND_TIMEOUT_SECONDS", cast=int, default=290
)
PAYOUT_POLICY_POOL_MAXSIZE = config(
    "PAYOUT_POLICY_POOL_MAXSIZE", cast=int, default=32
)
PAYOUT_POLICY_FAIL_CLOSED = config(
    "PAYOUT_POLICY_FAIL_CLOSED", cast=bool, default=True
)
PAYOUT_POLICY_REWARD_ENABLED = config(
    "PAYOUT_POLICY_REWARD_ENABLED", cast=bool, default=False
)
PAYOUT_POLICY_REWARD_URL = config(
    "PAYOUT_POLICY_REWARD_URL", default=""
)
PAYOUT_POLICY_REWARD_SOURCE_URL = config(
    "PAYOUT_POLICY_REWARD_SOURCE_URL", default=""
)
PAYOUT_POLICY_REWARD_SOURCE_TIMEOUT_SECONDS = config(
    "PAYOUT_POLICY_REWARD_SOURCE_TIMEOUT_SECONDS", cast=int, default=5
)
PAYOUT_POLICY_REWARD_TIMEOUT_SECONDS = config(
    "PAYOUT_POLICY_REWARD_TIMEOUT_SECONDS", cast=int, default=170
)


def _build_session():
    session = requests.Session()
    adapter = requests.adapters.HTTPAdapter(
        pool_connections=PAYOUT_POLICY_POOL_MAXSIZE,
        pool_maxsize=PAYOUT_POLICY_POOL_MAXSIZE,
    )
    session.mount("http://", adapter)
    session.mount("https://", adapter)
    session.trust_env = False
    return session


_SESSION = _build_session()


class PolicyPaymentResponse:
    def __init__(self, status, fee_msat=0, payment_preimage="", failure_reason=0):
        self.status = status
        self.fee_msat = fee_msat
        self.payment_preimage = payment_preimage
        self.failure_reason = failure_reason


def _call_policy(url, payload, timeout_seconds=None):
    if not url:
        raise RuntimeError("Payout policy middleware URL is not configured")
    if not PAYOUT_POLICY_TOKEN:
        raise RuntimeError("Payout policy middleware token is not configured")

    response = _SESSION.post(
        url,
        json=payload,
        headers={
            "Authorization": f"Bearer {PAYOUT_POLICY_TOKEN}",
            "Content-Type": "application/json",
        },
        timeout=_timeout(timeout_seconds or PAYOUT_POLICY_TIMEOUT_SECONDS),
    )

    try:
        data = response.json()
    except Exception:
        data = {"message": response.text}

    if response.status_code >= 400:
        raise RuntimeError(f"Payout policy middleware HTTP {response.status_code}: {data}")

    return data


def _timeout(read_timeout_seconds):
    return (
        float(PAYOUT_POLICY_CONNECT_TIMEOUT_SECONDS),
        float(read_timeout_seconds),
    )


def _ln_payload(lnpayment, order, fee_limit_sat, timeout_seconds):
    return {
        "request_id": f"lnpayment-{lnpayment.payment_hash}",
        "source": "robosats",
        "kind": "ln_payout",
        "order_id": order.id,
        "payment_hash": lnpayment.payment_hash,
        "payment_request": lnpayment.invoice,
        "amount_sats": int(lnpayment.num_satoshis),
        "fee_limit_sat": int(fee_limit_sat),
        "timeout_seconds": int(timeout_seconds),
        "routing_budget_ppm": int(lnpayment.routing_budget_ppm),
        "routing_budget_sats": str(lnpayment.routing_budget_sats),
        "lnpayment_status": int(lnpayment.status),
        "order_status": int(order.status),
        "receiver_user_id": lnpayment.receiver_id,
        "maker_user_id": order.maker_id,
        "taker_user_id": order.taker_id,
        "order_type": int(order.type),
        "attempt_number": int(lnpayment.routing_attempts) + 1,
    }


def _onchain_payload(onchainpayment, request):
    order = onchainpayment.order_paid_TX
    try:
        receiver_robot_id = onchainpayment.receiver.robot.id
    except Exception:
        receiver_robot_id = None

    return {
        "request_id": f"onchainpayment-{onchainpayment.id}",
        "source": "robosats",
        "kind": "onchain_payout",
        "order_id": order.id,
        "onchain_payment_id": onchainpayment.id,
        "address": request.addr,
        "amount_sats": int(request.amount),
        "fee_rate_sat_vbyte": int(request.sat_per_vbyte),
        "label": request.label,
        "spend_unconfirmed": bool(request.spend_unconfirmed),
        "gross_payout_sats": int(onchainpayment.num_satoshis),
        "mining_fee_sats": int(onchainpayment.mining_fee_sats),
        "swap_fee_rate": str(onchainpayment.swap_fee_rate),
        "receiver_user_id": onchainpayment.receiver_id,
        "receiver_robot_id": receiver_robot_id,
        "concept": "PAYBUYER",
        "onchain_payment_status": int(onchainpayment.status),
        "order_status": int(order.status),
        "is_swap": bool(order.is_swap),
    }


def _keysend_payload(
    target_pubkey, message, num_satoshis, routing_budget_sats, timeout, sign
):
    request_fingerprint = hashlib.sha256(
        "|".join(
            [
                str(target_pubkey),
                str(message),
                str(num_satoshis),
                str(routing_budget_sats),
                str(timeout),
            ]
        ).encode("utf-8")
    ).hexdigest()
    return {
        "request_id": f"keysend-devfund-{request_fingerprint}",
        "source": "robosats",
        "kind": "devfund_keysend",
        "target_pubkey": target_pubkey,
        "message": str(message),
        "amount_sats": int(num_satoshis),
        "fee_limit_sat": int(routing_budget_sats),
        "timeout_seconds": int(timeout),
        "sign": bool(sign),
    }


def _policy_payment_response(data, lightning_pb2):
    status = str(data.get("status", "")).upper()
    status_map = {
        "INITIATED": lightning_pb2.Payment.PaymentStatus.INITIATED,
        "IN_FLIGHT": lightning_pb2.Payment.PaymentStatus.IN_FLIGHT,
        "SUCCEEDED": lightning_pb2.Payment.PaymentStatus.SUCCEEDED,
        "ALREADY_PAID": lightning_pb2.Payment.PaymentStatus.SUCCEEDED,
        "FAILED": lightning_pb2.Payment.PaymentStatus.FAILED,
        "EXPIRED": lightning_pb2.Payment.PaymentStatus.FAILED,
        "POLICY_REJECTED": lightning_pb2.Payment.PaymentStatus.FAILED,
        "ERROR": lightning_pb2.Payment.PaymentStatus.FAILED,
    }
    fee_msat = data.get("fee_msat")
    if fee_msat is None and data.get("fee_sat") is not None:
        fee_msat = int(float(data.get("fee_sat")) * 1000)

    return PolicyPaymentResponse(
        status=status_map.get(status, lightning_pb2.Payment.PaymentStatus.FAILED),
        fee_msat=int(fee_msat or 0),
        payment_preimage=data.get("payment_preimage") or "",
        failure_reason=int(data.get("failure_reason_code") or 3),
    )


def _keysend_payment_response(data, LNPayment):
    status = str(data.get("status", "")).upper()
    status_map = {
        "INITIATED": LNPayment.Status.FLIGHT,
        "IN_FLIGHT": LNPayment.Status.FLIGHT,
        "SUCCEEDED": LNPayment.Status.SUCCED,
        "ALREADY_PAID": LNPayment.Status.SUCCED,
        "FAILED": LNPayment.Status.FAILRO,
        "POLICY_REJECTED": LNPayment.Status.FAILRO,
        "ERROR": LNPayment.Status.FAILRO,
    }

    payment_hash = data.get("payment_hash") or ""
    payment_preimage = data.get("payment_preimage") or ""
    if not payment_hash or not payment_preimage:
        raise RuntimeError("Payout policy middleware returned incomplete keysend data")

    if data.get("message") == "already handled" and LNPayment.objects.filter(
        payment_hash=payment_hash
    ).exists():
        return False, {}

    keysend_payment = {
        "created_at": timezone.now(),
        "expires_at": timezone.now(),
        "payment_hash": payment_hash,
        "preimage": payment_preimage,
        "fee": float(data.get("fee_sat") or 0),
        "status": status_map.get(status, LNPayment.Status.FAILRO),
    }
    if data.get("failure_reason_code") is not None:
        keysend_payment["failure_reason"] = int(data["failure_reason_code"])
    return True, keysend_payment


def _handle_expired_invoice(lnpayment, order):
    print(f"Order: {order.id}. INVOICE EXPIRED. Hash: {lnpayment.payment_hash}")
    lnpayment.status = type(lnpayment).Status.EXPIRE
    lnpayment.last_routing_time = timezone.now()
    lnpayment.in_flight = False
    lnpayment.save(update_fields=["status", "last_routing_time", "in_flight"])

    order.update_status(type(order).Status.FAI)
    order.expires_at = timezone.now() + timedelta(
        seconds=order.t_to_expire(type(order).Status.FAI)
    )
    order.save(update_fields=["expires_at"])

    order.log(
        f"Payment LNPayment({lnpayment.payment_hash},{str(lnpayment)}) <b>had expired</b>"
    )
    return {
        "succeded": False,
        "context": "The payout invoice has expired",
    }


def handle_ln_payment(
    lnpayment, order, fee_limit_sat, timeout_seconds, handle_response, lightning_pb2
):
    if not PAYOUT_POLICY_ENABLED:
        return None

    try:
        policy_response = _call_policy(
            PAYOUT_POLICY_URL,
            _ln_payload(lnpayment, order, fee_limit_sat, timeout_seconds),
        )
        policy_status = str(policy_response.get("status", "")).upper()
        policy_decision = str(policy_response.get("decision", "")).upper()

        if policy_status == "EXPIRED":
            return _handle_expired_invoice(lnpayment, order)

        if policy_decision == "REJECTED" or policy_status == "POLICY_REJECTED":
            from api.tasks import send_notification

            order.log(
                f"Payout policy middleware rejected LNPayment({lnpayment.payment_hash},{str(lnpayment)})",
                level="ERROR",
            )
            send_notification.delay(
                order_id=order.id, message="payout_policy_rejected"
            )
            return False

        if policy_decision == "ERROR" or policy_status == "ERROR":
            raise RuntimeError(policy_response)

        policy_lnd_response = _policy_payment_response(policy_response, lightning_pb2)
        return handle_response(
            policy_lnd_response,
            was_in_transit=policy_status in ["IN_FLIGHT", "INITIATED"],
        )

    except Exception as e:
        print(f"Payout policy middleware error: {e}")
        order.log(
            f"Payout policy middleware error for LNPayment({lnpayment.payment_hash},{str(lnpayment)}): {e}",
            level="ERROR",
        )
        if PAYOUT_POLICY_FAIL_CLOSED:
            return False
        return None


def handle_keysend_payment(
    target_pubkey,
    message,
    num_satoshis,
    routing_budget_sats,
    timeout,
    sign,
    LNPayment,
):
    if not PAYOUT_POLICY_KEYSEND_ENABLED:
        return None

    try:
        policy_response = _call_policy(
            PAYOUT_POLICY_KEYSEND_URL,
            _keysend_payload(
                target_pubkey,
                message,
                num_satoshis,
                routing_budget_sats,
                timeout,
                sign,
            ),
            PAYOUT_POLICY_KEYSEND_TIMEOUT_SECONDS,
        )
        policy_status = str(policy_response.get("status", "")).upper()
        policy_decision = str(policy_response.get("decision", "")).upper()

        if policy_decision == "REJECTED" or policy_status == "POLICY_REJECTED":
            return False, {}
        if policy_decision == "ERROR" or policy_status == "ERROR":
            raise RuntimeError(policy_response)

        return _keysend_payment_response(policy_response, LNPayment)

    except Exception as e:
        print(f"Keysend payout policy middleware error: {e}")
        if PAYOUT_POLICY_FAIL_CLOSED:
            return False, {}
        return None


def handle_onchain_payment(onchainpayment, request, on_mempool_code):
    if not PAYOUT_POLICY_ONCHAIN_ENABLED:
        return None

    try:
        policy_response = _call_policy(
            PAYOUT_POLICY_ONCHAIN_URL,
            _onchain_payload(onchainpayment, request),
        )
        policy_status = str(policy_response.get("status", "")).upper()
        policy_decision = str(policy_response.get("decision", "")).upper()

        if policy_decision == "REJECTED" or policy_status == "POLICY_REJECTED":
            from api.tasks import send_notification

            onchainpayment.order_paid_TX.log(
                f"Payout policy middleware rejected OnchainPayment({onchainpayment.id},{onchainpayment})",
                level="ERROR",
            )
            send_notification.delay(
                order_id=onchainpayment.order_paid_TX.id,
                message="payout_policy_rejected",
            )
            return False

        if policy_decision == "ERROR":
            raise RuntimeError(policy_response)

        if policy_status not in ["BROADCASTED", "ALREADY_BROADCASTED"]:
            raise RuntimeError(policy_response)

        txid = policy_response.get("txid")
        if not txid:
            raise RuntimeError("Payout policy middleware did not return txid")

        onchainpayment.status = on_mempool_code
        onchainpayment.txid = txid
        onchainpayment.broadcasted = True
        onchainpayment.save(update_fields=["status", "txid", "broadcasted"])
        onchainpayment.order_paid_TX.log(
            f"TX OnchainPayment({onchainpayment.id},{txid}) in <b>mempool</b>"
        )
        return True

    except Exception as e:
        print(f"Onchain payout policy middleware error: {e}")
        onchainpayment.order_paid_TX.log(
            f"Onchain payout policy middleware error for OnchainPayment({onchainpayment.id},{onchainpayment}): {e}",
            level="ERROR",
        )
        if PAYOUT_POLICY_FAIL_CLOSED:
            return False
        return None


def _reward_payload(lnpayment, fee_limit_sat, timeout_seconds):
    return {
        "request_id": f"reward-{lnpayment.payment_hash}",
        "source": "robosats",
        "kind": "reward_withdrawal",
        "payment_hash": lnpayment.payment_hash,
        "payment_request": lnpayment.invoice,
        "amount_sats": int(lnpayment.num_satoshis),
        "fee_limit_sat": int(fee_limit_sat),
        "timeout_seconds": int(timeout_seconds),
        "routing_budget_ppm": int(lnpayment.routing_budget_ppm),
        "routing_budget_sats": str(lnpayment.routing_budget_sats),
        "receiver_user_id": lnpayment.receiver_id,
        "receiver_robot_name": str(lnpayment.receiver.username),
    }


def _reward_source_payload(order, slashed_bond, staked_bond, reward, reason):
    rewarded_user = staked_bond.sender
    rewarded_robot = rewarded_user.robot
    if slashed_bond is None and reason == "slashed_bond":
        reason = "counterparty_cancelled"
    slashed_bond_hash = ""
    slashed_bond_sats = 0
    slashed_bond_status = 0
    if slashed_bond is not None:
        slashed_bond_hash = str(slashed_bond.payment_hash)
        slashed_bond_sats = int(slashed_bond.num_satoshis)
        slashed_bond_status = int(slashed_bond.status)
    return {
        "request_id": (
            f"reward-source-{order.id}-{rewarded_robot.id}-"
            f"{slashed_bond_hash or 'no-slashed-bond'}-{staked_bond.payment_hash}-{int(reward)}"
        ),
        "source": "robosats",
        "kind": "reward_source",
        "source_order_id": int(order.id),
        "receiver_robot_id": int(rewarded_robot.id),
        "receiver_user_id": int(rewarded_user.id),
        "receiver_robot_name": str(rewarded_user.username),
        "reward_sats": int(reward),
        "slashed_bond_payment_hash": slashed_bond_hash,
        "slashed_bond_sats": slashed_bond_sats,
        "slashed_bond_status": slashed_bond_status,
        "staked_bond_payment_hash": str(staked_bond.payment_hash),
        "staked_bond_sats": int(staked_bond.num_satoshis),
        "staked_bond_status": int(staked_bond.status),
        "reason": str(reason or "slashed_bond"),
    }


def record_reward_source(order, slashed_bond, staked_bond, reward, reason="slashed_bond"):
    if not PAYOUT_POLICY_REWARD_ENABLED:
        return None

    try:
        policy_response = _call_policy(
            PAYOUT_POLICY_REWARD_SOURCE_URL,
            _reward_source_payload(order, slashed_bond, staked_bond, reward, reason),
            PAYOUT_POLICY_REWARD_SOURCE_TIMEOUT_SECONDS,
        )
        if not policy_response.get("ok"):
            raise RuntimeError(policy_response)
        return True

    except Exception as e:
        print(f"Reward source policy middleware error: {e}")
        try:
            order.log(
                f"Reward source policy middleware error for Order({order.id}): {e}",
                level="ERROR",
            )
        except Exception:
            pass
        if PAYOUT_POLICY_FAIL_CLOSED:
            return False
        return None


def handle_reward_withdrawal(lnpayment, fee_limit_sat, timeout_seconds):
    if not PAYOUT_POLICY_REWARD_ENABLED:
        return None

    try:
        policy_response = _call_policy(
            PAYOUT_POLICY_REWARD_URL,
            _reward_payload(lnpayment, fee_limit_sat, timeout_seconds),
            timeout_seconds or PAYOUT_POLICY_REWARD_TIMEOUT_SECONDS,
        )
        policy_status = str(policy_response.get("status", "")).upper()
        policy_decision = str(policy_response.get("decision", "")).upper()

        if policy_status == "EXPIRED":
            from api.models import LNPayment
            lnpayment.status = LNPayment.Status.EXPIRE
            lnpayment.last_routing_time = timezone.now()
            lnpayment.in_flight = False
            lnpayment.save(update_fields=["status", "last_routing_time", "in_flight"])
            return {
                "succeded": False,
                "context": "The reward payout invoice has expired",
            }

        if policy_decision == "REJECTED" or policy_status == "POLICY_REJECTED":
            print(
                f"Payout policy middleware rejected reward LNPayment({lnpayment.payment_hash},{str(lnpayment)})",
            )
            return False

        if policy_decision == "ERROR" or policy_status == "ERROR":
            raise RuntimeError(policy_response)

        status = str(policy_response.get("status", "")).upper()
        if status in ["INITIATED", "IN_FLIGHT"]:
            lnpayment.status = type(lnpayment).Status.FLIGHT
            lnpayment.in_flight = True
            lnpayment.last_routing_time = timezone.now()
            lnpayment.save(update_fields=["status", "in_flight", "last_routing_time"])
            return {
                "succeded": False,
                "in_flight": True,
                "context": "The reward payout is still in flight",
            }

        if status in ["SUCCEEDED", "ALREADY_PAID"]:
            lnpayment.status = type(lnpayment).Status.SUCCED
            lnpayment.in_flight = False
            lnpayment.fee = float(policy_response.get("fee_msat") or 0) / 1000
            lnpayment.preimage = policy_response.get("payment_preimage") or ""
            lnpayment.save(update_fields=["status", "in_flight", "fee", "preimage"])
            return {
                "succeded": True,
                "payment_hash": policy_response.get("payment_hash") or "",
                "payment_preimage": policy_response.get("payment_preimage") or "",
                "fee_msat": int(policy_response.get("fee_msat") or 0),
                "status": "SUCCEEDED",
                "failure_reason": int(policy_response.get("failure_reason_code") or 0),
            }

        lnpayment.status = type(lnpayment).Status.FAILRO
        lnpayment.in_flight = False
        lnpayment.failure_reason = int(policy_response.get("failure_reason_code") or 3)
        lnpayment.save(update_fields=["status", "in_flight", "failure_reason"])
        return {
            "succeded": False,
            "payment_hash": policy_response.get("payment_hash") or "",
            "payment_preimage": policy_response.get("payment_preimage") or "",
            "fee_msat": int(policy_response.get("fee_msat") or 0),
            "status": "FAILED",
            "failure_reason": int(policy_response.get("failure_reason_code") or 3),
        }

    except Exception as e:
        print(f"Reward payout policy middleware error: {e}")
        if PAYOUT_POLICY_FAIL_CLOSED:
            return False
        return None
