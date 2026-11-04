# Build from the repository root:
#   docker build -f server/deploy/Dockerfile -t meshmsg-relay .
#   docker run -v $PWD/relay.json:/etc/meshmsg/relay.json -v relaydata:/data -p 8080:8080 meshmsg-relay
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/meshmsg-relay ./server/cmd/relay

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/meshmsg-relay /meshmsg-relay
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/meshmsg-relay", "-config", "/etc/meshmsg/relay.json"]
