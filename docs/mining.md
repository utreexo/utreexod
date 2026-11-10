# Mining

utreexod supports the `getblocktemplate` RPC.
The limited user cannot access this RPC.

## Add the payment addresses with the `miningaddr` option

```bash
[Application Options]
rpcuser=myuser
rpcpass=SomeDecentp4ssw0rd
miningaddr=12c6DSiU4Rq3P4ZxziKxzrL5LmMBrzjrJX
miningaddr=1M83ju3EChKYyysmM2FXtLNftbacagd8FR
```

## Add utreexod's RPC TLS certificate to system Certificate Authority list

`cgminer` uses [curl](http://curl.haxx.se/) to fetch data from the RPC server.
Since curl validates the certificate by default, we must install the `utreexod` RPC
certificate into the default system Certificate Authority list.

## Ubuntu

1. Copy rpc.cert to /usr/share/ca-certificates: `# cp /home/user/.utreexod/rpc.cert /usr/share/ca-certificates/utreexod.crt`
2. Add utreexod.crt to /etc/ca-certificates.conf: `# echo utreexod.crt >> /etc/ca-certificates.conf`
3. Update the CA certificate list: `# update-ca-certificates`

## Set your mining software url to use https

`cgminer -o https://127.0.0.1:8334 -u rpcuser -p rpcpassword`
