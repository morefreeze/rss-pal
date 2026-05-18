# Local TLS certs

`docker-compose` mounts this directory into nginx for HTTPS on port 8443.
nginx expects `localhost+2.pem` and `localhost+2-key.pem` (the default names
[mkcert](https://github.com/FiloSottile/mkcert) produces for `localhost 127.0.0.1 ::1`).
The `.pem` files are gitignored — generate them locally:

```bash
brew install mkcert nss   # nss is needed for Firefox trust
mkcert -install           # one-time: installs a local root CA into your trust store
cd certs && mkcert localhost 127.0.0.1 ::1
```

Then `docker-compose up -d --build frontend` and visit <https://localhost:8443>.

Re-run the `mkcert localhost 127.0.0.1 ::1` command if the cert expires (default ~2 years).
