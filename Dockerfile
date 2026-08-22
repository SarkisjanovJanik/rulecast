# syntax=docker/dockerfile:1
FROM golang:1.25-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/rulecast ./cmd/rulecast

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/rulecast /rulecast
COPY web /web
USER nonroot:nonroot
EXPOSE 8090
ENV RULECAST_WEB_ROOT=/web
ENTRYPOINT ["/rulecast"]
