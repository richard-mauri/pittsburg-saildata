FROM debian:trixie-slim
RUN apt-get update && apt-get install -y --no-install-recommends osmium-tool=1.18.0-1 && rm -rf /var/lib/apt/lists/*
COPY extract.sh /usr/local/bin/poi-extract
RUN chmod 0555 /usr/local/bin/poi-extract
ENTRYPOINT ["/usr/local/bin/poi-extract"]
