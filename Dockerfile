FROM tsuzu:gmusic

WORKDIR /tmp
RUN wget https://redirector.gvt1.com/edgedl/go/go1.9.2.linux-amd64.tar.gz && tar zxvf go*.tar.gz
RUN mv go* /usr/local/go && RUN mkdir /root/go
RUN echo 'export GOPATH="/root/go"' >> ~/.bashrc
RUN echo 'export GOROOT="/usr/local/go"' >> ~/.bashrc
RUN echo 'export PATH=$GOROOT/bin:$GOPATH/bin:$PATH' >> ~/.bashrc
RUN go get github.com/cs3238-tsuzu/recs4m
WORKDIR /root

ENTRYPOINT ["recs4m"]