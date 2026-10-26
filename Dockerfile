# Stage 1: Bundle frontend assets
FROM oven/bun:1-alpine AS web-builder
WORKDIR /build
COPY scripts/ ./scripts/
COPY web/ ./web/
RUN bun ./scripts/build-web.js

# Stage 2: Build static Go binary
FROM golang:1.25-alpine AS go-builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Copy the generated bundles into web/
COPY --from=web-builder /build/web/app.js ./web/app.js
COPY --from=web-builder /build/web/workbench.js ./web/workbench.js
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /bin/punk0 .

# Stage 3: Minimal runtime container
FROM alpine:3.21
RUN apk --no-cache add ca-certificates git
COPY --from=go-builder /bin/punk0 /usr/local/bin/punk0

EXPOSE 7777
ENTRYPOINT ["punk0", "-host", "0.0.0.0", "-no-open"]
CMD ["/workspace"]
