# ---- Build stage -----------------------------------------------
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Cache dependencies first.
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /server ./cmd/server

# ---- Runtime stage ---------------------------------------------
FROM alpine:3.21

# ca-certificates needed for HTTPS calls to Firebase / Firestore.
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /server .

# Cloud Run sets PORT automatically.
ENV PORT=8080

EXPOSE 8080

USER nobody
ENTRYPOINT ["/app/server"]
