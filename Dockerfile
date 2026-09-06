# SPDX-License-Identifier: Apache-2.0
FROM golang:1.26.6-alpine AS build

WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown

RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
    -o /out/voicy ./cmd/voicy

FROM alpine:3.24

# ffmpeg extracts the audio track from video and from oversized recordings
# before they reach Deepgram, which is what Deepgram recommends for large video
# and what keeps a hundred-megabyte upload from crossing the network twice.
RUN apk add --no-cache ca-certificates ffmpeg \
    && adduser -D -H -u 10001 voicy

WORKDIR /app

COPY --from=build /out/voicy /app/voicy
COPY migrations /app/migrations

USER voicy

EXPOSE 8080

ENTRYPOINT ["/app/voicy"]
