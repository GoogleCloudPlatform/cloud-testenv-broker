VERSION=3.0.0-beta-2
ZIPFILE=protoc-${VERSION}-linux-x86_64.zip

wget https://github.com/google/protobuf/releases/download/v${VERSION}/${ZIPFILE}
unzip $ZIPFILE -d $HOME/protobuf
