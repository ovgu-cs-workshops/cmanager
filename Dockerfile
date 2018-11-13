FROM embeddedenterprises/burrow as builder
RUN apk update && apk add build-base
RUN burrow clone https://github.com/ovgu-cs-workshops/cmanager.git
WORKDIR $GOPATH/src/github.com/ovgu-cs-workshops/cmanager
RUN burrow e && burrow b
RUN cp bin/cmanager /bin

FROM scratch
LABEL service "cmanager"
LABEL vendor "ovgu-cs-workshops"
LABEL maintainers "Martin Koppehel <mkoppehel@embedded.enterprises>"

COPY --from=builder /bin/cmanager /bin/cmanager
ENTRYPOINT ["/bin/cmanager"]
CMD []
