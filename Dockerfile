FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY discovery/ discovery/
COPY skillhub/ skillhub/
RUN cd discovery && go build -o /discovery ./cmd/discovery/
RUN cd skillhub && go build -o /skillhub ./cmd/skillhub/

FROM alpine:3.19
RUN apk add --no-cache ca-certificates clamav
COPY --from=builder /discovery /usr/local/bin/discovery
COPY --from=builder /skillhub /usr/local/bin/skillhub
EXPOSE 8399
CMD ["discovery"]
