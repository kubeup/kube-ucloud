FROM debian:jessie
RUN apt update && apt install -y ca-certificates
ADD ucloud-controller /ucloud-controller
