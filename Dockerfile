
FROM golang:1.18 AS BuildStage

WORKDIR /app

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o app ./...

FROM alpine:latest AS production

COPY --from=BuildStage /app .

CMD ["./app"]

 