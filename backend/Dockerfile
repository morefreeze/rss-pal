# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

ENV GOPROXY=https://goproxy.cn,direct

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -o backfill_image_dimensions ./cmd/backfill_image_dimensions

# Runtime stage
FROM alpine:latest

# KEEP IN SYNC WITH backend/Dockerfile.worker runtime packages.
RUN apk --no-cache add ca-certificates tzdata \
    yt-dlp \
    poppler-utils \
    tesseract-ocr \
    tesseract-ocr-data-eng \
    tesseract-ocr-data-chi_sim

ARG BGUTIL_PROVIDER_VERSION=1.3.1
ARG BGUTIL_PROVIDER_SHA256=b8ceec7f76143da172aaf5ebeec0c2d218e5680c063b931586bca48567069b38
RUN mkdir -p /root/.config/yt-dlp/plugins \
    && wget -qO /root/.config/yt-dlp/plugins/bgutil-ytdlp-pot-provider.zip \
        "https://github.com/Brainicism/bgutil-ytdlp-pot-provider/releases/download/${BGUTIL_PROVIDER_VERSION}/bgutil-ytdlp-pot-provider.zip" \
    && echo "${BGUTIL_PROVIDER_SHA256}  /root/.config/yt-dlp/plugins/bgutil-ytdlp-pot-provider.zip" \
        | sha256sum -c -

WORKDIR /app

COPY --from=builder /app/server .
COPY --from=builder /app/backfill_image_dimensions .

EXPOSE 8080

CMD ["./server"]
