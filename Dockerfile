# Hopper control plane (hopperd) — multi-stage, static, CGO-free.
FROM golang:1.22-alpine AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build \
    -ldflags "-s -w -X github.com/mcpeixoto/hopper/internal/version.Version=${VERSION}" \
    -o /bin/hopperd ./cmd/hopperd

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
RUN mkdir -p data
COPY --from=build /bin/hopperd .
EXPOSE 8080
CMD ["./hopperd"]
