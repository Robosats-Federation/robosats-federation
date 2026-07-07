FROM golang:1.26-bookworm AS build

ARG LND_VERSION

WORKDIR /src

COPY go.mod go.sum ./

RUN test -n "${LND_VERSION}" || (echo "LND_VERSION build arg is required" >&2; exit 1)
RUN go get "github.com/lightningnetwork/lnd@${LND_VERSION}" && \
    go mod download && \
    test "$(go list -m -f '{{.Version}}' github.com/lightningnetwork/lnd)" = "${LND_VERSION}"

COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/payout-policy ./cmd/payout-policy

FROM gcr.io/distroless/static-debian12

ENV HOST=0.0.0.0
ENV PORT=8088

WORKDIR /app

COPY --from=build /out/payout-policy /app/payout-policy
COPY migrations /app/migrations

CMD ["/app/payout-policy"]
