import requests

from decouple import config
from django.utils.dateparse import parse_datetime


LND_ADAPTER_ENABLED = config("LND_ADAPTER_ENABLED", cast=bool, default=False)
LND_ADAPTER_URL = config("LND_ADAPTER_URL", default="")
LND_ADAPTER_TOKEN = config("LND_ADAPTER_TOKEN", default="")
LND_ADAPTER_TIMEOUT_SECONDS = config(
    "LND_ADAPTER_TIMEOUT_SECONDS", cast=int, default=10
)
LND_ADAPTER_CONNECT_TIMEOUT_SECONDS = config(
    "LND_ADAPTER_CONNECT_TIMEOUT_SECONDS", cast=float, default=2.0
)
LND_ADAPTER_PAYMENT_TIMEOUT_SECONDS = config(
    "LND_ADAPTER_PAYMENT_TIMEOUT_SECONDS", cast=int, default=170
)
LND_ADAPTER_POOL_MAXSIZE = config(
    "LND_ADAPTER_POOL_MAXSIZE", cast=int, default=32
)


def _build_session():
    session = requests.Session()
    adapter = requests.adapters.HTTPAdapter(
        pool_connections=LND_ADAPTER_POOL_MAXSIZE,
        pool_maxsize=LND_ADAPTER_POOL_MAXSIZE,
    )
    session.mount("http://", adapter)
    session.mount("https://", adapter)
    session.trust_env = False
    return session


_SESSION = _build_session()


class AdapterPayReq:
    def __init__(self, data):
        self.destination = data.get("destination", "")
        self.payment_hash = data.get("payment_hash", "")
        self.num_satoshis = int(data.get("num_satoshis") or 0)
        self.timestamp = int(data.get("timestamp") or 0)
        self.expiry = int(data.get("expiry") or 0)
        self.description = data.get("description", "")
        self.description_hash = data.get("description_hash", "")
        self.fallback_addr = data.get("fallback_addr", "")
        self.cltv_expiry = int(data.get("cltv_expiry") or 0)
        self.route_hints = [
            AdapterRouteHint(route_hint)
            for route_hint in data.get("route_hints", [])
        ]
        self.payment_addr = data.get("payment_addr", "")
        self.num_msat = int(data.get("num_msat") or 0)


class AdapterRouteHint:
    def __init__(self, data):
        self.hop_hints = [
            AdapterHopHint(hop_hint)
            for hop_hint in data.get("hop_hints", [])
        ]


class AdapterHopHint:
    def __init__(self, data):
        self.node_id = data.get("node_id", "")
        self.chan_id = int(data.get("chan_id") or 0)
        self.fee_base_msat = int(data.get("fee_base_msat") or 0)
        self.fee_proportional_millionths = int(
            data.get("fee_proportional_millionths") or 0
        )
        self.cltv_expiry_delta = int(data.get("cltv_expiry_delta") or 0)


class AdapterFee:
    def __init__(self, data):
        self.fee_sat = int(data.get("fee_sat") or 0)
        self.sat_per_vbyte = int(data.get("sat_per_vbyte") or 0)


class AdapterWalletBalance:
    def __init__(self, data):
        self.total_balance = int(data.get("total_balance") or 0)
        self.confirmed_balance = int(data.get("confirmed_balance") or 0)
        self.unconfirmed_balance = int(data.get("unconfirmed_balance") or 0)
        self.locked_balance = int(data.get("locked_balance") or 0)
        self.reserved_balance_anchor_chan = int(
            data.get("reserved_balance_anchor_chan") or 0
        )


class AdapterChannelBalance:
    def __init__(self, data):
        self.balance = int(data.get("balance") or 0)
        self.pending_open_balance = int(data.get("pending_open_balance") or 0)
        self.local_balance = AdapterAmount(data.get("local_balance_sat") or 0)
        self.remote_balance = AdapterAmount(data.get("remote_balance_sat") or 0)
        self.unsettled_local_balance = AdapterAmount(
            data.get("unsettled_local_balance_sat") or 0
        )
        self.unsettled_remote_balance = AdapterAmount(
            data.get("unsettled_remote_balance_sat") or 0
        )


class AdapterAmount:
    def __init__(self, sats):
        self.sat = int(sats or 0)
        self.msat = self.sat * 1000


class AdapterInvoice:
    def __init__(self, data):
        self.payment_request = data.get("payment_request", "")
        self.payment_hash = data.get("payment_hash", "")
        self.preimage = data.get("preimage", "")
        self.created_at = parse_datetime(data.get("created_at", ""))
        self.expires_at = parse_datetime(data.get("expires_at", ""))
        self.cltv_expiry = int(data.get("cltv_expiry") or 0)


class AdapterInvoiceLookup:
    def __init__(self, data):
        self.found = bool(data.get("found"))
        self.state = data.get("state", "UNKNOWN")
        self.state_code = int(data.get("state_code") or 0)
        self.payment_hash = data.get("payment_hash", "")
        self.payment_request = data.get("payment_request", "")
        self.value_sat = int(data.get("value_sat") or 0)
        self.amt_paid_sat = int(data.get("amt_paid_sat") or 0)
        self.amt_paid_msat = int(data.get("amt_paid_msat") or 0)
        self.creation_date = int(data.get("creation_date") or 0)
        self.settle_date = int(data.get("settle_date") or 0)
        self.expiry = int(data.get("expiry") or 0)
        self.expiry_height = int(data.get("expiry_height") or 0)
        self.htlcs = [
            AdapterInvoiceHTLC(htlc)
            for htlc in data.get("htlcs", [])
        ]


class AdapterInvoiceHTLC:
    def __init__(self, data):
        self.chan_id = int(data.get("chan_id") or 0)
        self.htlc_index = int(data.get("htlc_index") or 0)
        self.amt_msat = int(data.get("amt_msat") or 0)
        self.accept_height = int(data.get("accept_height") or 0)
        self.expiry_height = int(data.get("expiry_height") or 0)
        self.state = data.get("state", "UNKNOWN")
        self.state_code = int(data.get("state_code") or 0)


def get_version():
    data = _get("/v1/robosats/lnd/version")
    return data.get("version", "")


def decode_payreq(payment_request):
    return AdapterPayReq(
        _post(
            "/v1/robosats/lnd/decode-payreq",
            {"payment_request": payment_request},
        )
    )


def estimate_fee(amount_sats, target_conf, min_confs):
    return AdapterFee(
        _post(
            "/v1/robosats/lnd/estimate-fee",
            {
                "amount_sats": int(amount_sats),
                "target_conf": int(target_conf),
                "min_confs": int(min_confs),
            },
        )
    )


def wallet_balance():
    return AdapterWalletBalance(_get("/v1/robosats/lnd/wallet-balance"))


def channel_balance():
    return AdapterChannelBalance(_get("/v1/robosats/lnd/channel-balance"))


def gen_hold_invoice(memo, amount_sats, invoice_expiry, cltv_expiry):
    return AdapterInvoice(
        _post(
            "/v1/robosats/lnd/hold-invoice",
            {
                "memo": memo,
                "amount_sats": int(amount_sats),
                "invoice_expiry": int(invoice_expiry),
                "cltv_expiry": int(cltv_expiry),
            },
        )
    )


def cancel_return_hold_invoice(payment_hash):
    return _post(
        "/v1/robosats/lnd/cancel-invoice",
        {"payment_hash": payment_hash},
    )


def settle_hold_invoice(preimage):
    return _post(
        "/v1/robosats/lnd/settle-invoice",
        {"preimage": preimage},
    )


def lookup_invoice(payment_hash):
    return AdapterInvoiceLookup(
        _post(
            "/v1/robosats/lnd/lookup-invoice",
            {"payment_hash": payment_hash},
        )
    )


def _get(path, timeout_seconds=None):
    return _request("GET", path, None, timeout_seconds)


def _post(path, payload, timeout_seconds=None):
    return _request("POST", path, payload, timeout_seconds)


def _request(method, path, payload=None, timeout_seconds=None):
    if not LND_ADAPTER_URL:
        raise RuntimeError("LND adapter URL is not configured")
    if not LND_ADAPTER_TOKEN:
        raise RuntimeError("LND adapter token is not configured")

    response = _SESSION.request(
        method,
        LND_ADAPTER_URL.rstrip("/") + path,
        json=payload,
        headers={
            "Authorization": f"Bearer {LND_ADAPTER_TOKEN}",
            "Content-Type": "application/json",
        },
        timeout=_timeout(timeout_seconds or LND_ADAPTER_TIMEOUT_SECONDS),
    )

    try:
        data = response.json()
    except Exception:
        data = {"message": response.text}

    if response.status_code >= 400:
        raise RuntimeError(f"LND adapter HTTP {response.status_code}: {data}")

    return data


def _timeout(read_timeout_seconds):
    return (
        float(LND_ADAPTER_CONNECT_TIMEOUT_SECONDS),
        float(read_timeout_seconds),
    )
