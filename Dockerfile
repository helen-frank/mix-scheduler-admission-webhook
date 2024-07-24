FROM golang:1.22.5-alpine3.20 AS builder
WORKDIR /usr/src/app
# ENV GOPROXY https://goproxy.io,direct
# RUN go mod download
COPY . .
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories && apk add --no-cache upx ca-certificates tzdata
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o mix-scheduler-admission-webhook . && upx mix-scheduler-admission-webhook

FROM alpine:3.20 as runner
COPY --from=builder /usr/share/zoneinfo/Asia/Shanghai /etc/localtime
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/src/app/mix-scheduler-admission-webhook /usr/local/bin/

CMD ["mix-scheduler-admission-webhook"]
