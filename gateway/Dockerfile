FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /gateway ./cmd/gateway
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /mcp ./cmd/mcp
FROM scratch
COPY --from=build /gateway /gateway
COPY --from=build /mcp /mcp
COPY --from=build /src/policy.json /gateway/policy.json
COPY --from=build /src/migrations /gateway/migrations
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
EXPOSE 8080
ENV PORT=8080
ENTRYPOINT ["/gateway"]
