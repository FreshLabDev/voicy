# SPDX-License-Identifier: Apache-2.0
FROM golang:1.26.5-alpine AS build

WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
    -o /out/voicetotext ./cmd/voicetotext

FROM alpine:3.22
RUN apk add --no-cache ca-certificates \
    && adduser -D -H -u 10001 voicetotext
WORKDIR /app
COPY --from=build /out/voicetotext /app/voicetotext
COPY migrations /app/migrations
USER voicetotext
EXPOSE 8080
ENTRYPOINT ["/app/voicetotext"]
