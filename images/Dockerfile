FROM kindest/node:v1.21.1

RUN apt-get update -yq && \
    apt-get install -yq --no-install-recommends lsb-release dnsutils gpg wget

RUN curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor -o /usr/share/keyrings/docker-archive-keyring.gpg && \
    echo \
      "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/docker-archive-keyring.gpg] https://download.docker.com/linux/ubuntu \
      $(lsb_release -cs) stable" | tee /etc/apt/sources.list.d/docker.list > /dev/null && \
    apt-get update -yq && \
    apt-get install -yq --no-install-recommends docker-ce docker-ce-cli --option=Dpkg::Options::=--force-confdef
    ## TODO(AC): this is quite optimistic, fix this

RUN rm -f /etc/systemd/system/kubelet.service && \
    rm -rf /etc/systemd/system/kubelet.service.d/

COPY run-userdata.sh /run-userdata.sh
COPY run-userdata.service /etc/systemd/system

RUN systemctl enable run-userdata.service

ENTRYPOINT ["/usr/local/bin/entrypoint", "/sbin/init"]
