FROM kindest/node:v1.21.1

RUN rm -f /etc/systemd/system/kubelet.service && \
    rm -rf /etc/systemd/system/kubelet.service.d/

COPY run-userdata.sh /run-userdata.sh
COPY run-userdata.service /etc/systemd/system

RUN systemctl enable run-userdata.service

ENTRYPOINT ["/usr/local/bin/entrypoint", "/sbin/init"]
