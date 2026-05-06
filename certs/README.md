# Local TLS certs

`docker-compose` mounts `cert.pem` and `key.pem` from this directory into
nginx for HTTPS on port 443. The `.pem` files are gitignored — generate them
locally with [mkcert](https://github.com/FiloSottile/mkcert):

```bash
brew install mkcert nss   # nss is needed for Firefox trust
mkcert -install           # one-time: installs a local root CA into your trust store
mkcert -cert-file certs/cert.pem -key-file certs/key.pem localhost 127.0.0.1 ::1
```

Then `docker-compose up -d --build frontend` and visit <https://localhost>.

Re-run the `mkcert -cert-file ...` command if the cert expires (default ~2 years).
