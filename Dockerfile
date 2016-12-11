FROM golang:1.7.4
# RUN apk update && apk add py-pip && pip install docker-compose
ADD . /go/src/github.com/netice9/wheeltapper
WORKDIR /go/src/github.com/netice9/wheeltapper
RUN go install .
RUN mv /go/bin/wheeltapper / && rm -rf /go /usr/local/go && rm -rf /usr/lib && rm -rf /usr/local && rm -rf /usr/share
RUN mkdir /work
WORKDIR /work
ENTRYPOINT ["/wheeltapper"]
