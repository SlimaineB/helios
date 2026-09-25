# Registers instances + demo application with the API and writes per-agent
# config files into the shared /conf volume (mounted as /etc/ansiblehub).
FROM alpine:3.20
RUN apk add --no-cache curl jq
COPY hack/bootstrap/bootstrap.sh /usr/local/bin/bootstrap.sh
RUN chmod +x /usr/local/bin/bootstrap.sh
CMD ["/usr/local/bin/bootstrap.sh"]