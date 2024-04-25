cd $HOME

curl -LO https://musl.cc/aarch64-linux-musl-cross.tgz
curl -LO https://musl.cc/arm-linux-musleabihf-cross.tgz
curl -LO https://musl.cc/armv6-linux-musleabihf-cross.tgz
curl -LO https://musl.cc/armv7l-linux-musleabihf-cross.tgz
curl -LO https://musl.cc/x86_64-linux-musl-cross.tgz
curl -LO https://musl.cc/x86_64-w64-mingw32-cross.tgz

tar -xzf aarch64-linux-musl-cross.tgz
tar -xzf arm-linux-musleabihf-cross.tgz
tar -xzf armv6-linux-musleabihf-cross.tgz
tar -xzf armv7l-linux-musleabihf-cross.tgz
tar -xzf x86_64-linux-musl-cross.tgz
tar -xzf x86_64-w64-mingw32-cross.tgz

rm aarch64-linux-musl-cross.tgz
rm arm-linux-musleabihf-cross.tgz
rm armv6-linux-musleabihf-cross.tgz
rm armv7l-linux-musleabihf-cross.tgz
rm x86_64-linux-musl-cross.tgz
rm x86_64-w64-mingw32-cross.tgz

echo export PATH=\$PATH:${HOME}/arm-linux-musleabihf-cross/bin >> $HOME/.profile
echo export PATH=\$PATH:${HOME}/armv6-linux-musleabihf-cross/bin >> $HOME/.profile
echo export PATH=\$PATH:${HOME}/armv7l-linux-musleabihf-cross/bin >> $HOME/.profile
echo export PATH=\$PATH:${HOME}/aarch64-linux-musl-cross/bin >> $HOME/.profile
echo export PATH=\$PATH:${HOME}/x86_64-linux-musl-cross/bin >> $HOME/.profile
echo export PATH=\$PATH:${HOME}/x86_64-w64-mingw32-cross/bin >> $HOME/.profile

echo "To add the binary paths:" ". $HOME/.profile"
