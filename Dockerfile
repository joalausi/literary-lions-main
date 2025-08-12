# syntax=docker/dockerfile:1

########################
# Build stage
########################
FROM golang:1.23.1 AS build
WORKDIR /app

# Use Go module proxy for reliability
ENV GOPROXY=https://proxy.golang.org,direct

# Copy module files first (cache layer)
COPY go.mod go.sum ./

# Show Go env and do a verbose download to surface errors
RUN go env && go mod download -x

# Copy the rest of the source
COPY . .

# Build static linux binary (modernc sqlite is pure Go; no CGO needed)
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
RUN go build -o /app/forumd ./cmd/forumd

########################
# Runtime stage
########################
FROM gcr.io/distroless/static:nonroot
WORKDIR /app

# copy binary + web assets/templates
COPY --from=build /app/forumd /app/forumd
COPY --from=build /app/web /app/web

# default port (same as code)
EXPOSE 8080
USER nonroot:nonroot

# OCI labels (metadata)
LABEL org.opencontainers.image.title="Literary Lions Forum" \
      org.opencontainers.image.description="Book club forum (Go + SQLite)" \
      org.opencontainers.image.source="https://gitea.kood.tech/mohammadtavasoli/Literary-lions.git" \
      org.opencontainers.image.version="0.1.0" \
      org.opencontainers.image.authors="Your Name"

ENTRYPOINT ["/app/forumd"]
