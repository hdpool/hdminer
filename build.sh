
export GOPATH="/root/go"
export GOROOT="/usr/local/go"

# cat hdpool3232.ico | $GOPATH/bin/2goarray trayicon main > hdpool3232_windows.go
# $GOPATH/bin/rsrc -manifest hdpool3232.manifest -arch amd64 -ico hdpool3232.ico -o hdpool3232_windows.syso

TARGET="build-all"
rm -rf ${TARGET} && mkdir ${TARGET}

# windows
rm -f libminer_windows.a;
(cd miner; PRECC=x86_64-w64-mingw32- sh -x ./b.sh);
CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc ${GOROOT}/bin/go build -ldflags "-s -w -H windowsgui" -o ${TARGET}/hdminer.exe

# linux 
rm -f libminer.a 
(cd miner; sh -x ./b.sh)
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 ${GOROOT}/bin/go build -ldflags "-s -w" -o ${TARGET}/hdproxy.linux

# darwin
# rm -f libminer_darwin.a
# (cd miner; sh -x ./b.sh)
# CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 ${GOROOT}/bin/go build -gccgoflags "-s -w" -o ${TARGET}/hdproxy.mac

# upx 
(cd ${TARGET}; echo 'hdproxy.exe center_proxy.exe' | xargs -n 1 cp hdminer.exe)
(cd ${TARGET}; echo 'center_proxy.linux' | xargs -n 1 cp hdproxy.linux)
# (cd ${TARGET}; echo 'center_proxy.mac' | xargs -n 1 cp hdproxy.mac)
