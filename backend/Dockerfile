# syntax=docker/dockerfile:1
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/indexer ./cmd/indexer

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/server /server
COPY --from=build /out/indexer /indexer
COPY data/stops.json /data/stops.json
ENV STOPS_INDEX_PATH=/data/stops.json
EXPOSE 8080
ENTRYPOINT ["/server"]
