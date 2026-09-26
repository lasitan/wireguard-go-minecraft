# Master UI (TypeScript source)

Build into the Go embed directory:

```bash
cd web
npm install
npm run build   # writes to ../master/ui
```

The committed `master/ui/` assets are used by `go:embed` so Debian/CI builds
do not require Node.js. Re-run `npm run build` after UI changes and commit
the updated `master/ui` files.
