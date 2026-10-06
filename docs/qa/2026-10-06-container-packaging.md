# Container packaging verification — October 6, 2026

The Docker socket was unavailable, so rootless Podman built and ran the existing Dockerfiles locally. Tested source was `2a0a295` plus the packaging changes committed with this record. Image tags stayed local; nothing was pushed or deployed.

The first web build compiled successfully, then failed resolving the unqualified `nginx:1.29-alpine` runtime image. Dockerfiles now qualify all existing base images with `docker.io/library/`, retaining their versions. Context-local `.dockerignore` files exclude environment files and generated artifacts; frontend `node_modules` (138 MB locally) and `dist` are excluded as well. README and agent guidance describe these packaging conventions.

| Gate | Observed evidence |
| --- | --- |
| API image build | `podman build --build-arg BUILD_COMMIT=2a0a295 -t localhost/overlaid-9734-api:qa backend` passed. Runtime image ID begins `35672bbce229`; `/version` reported Go 1.26.8, matching the declared Go 1.26 baseline. |
| Web image build | `podman build -t localhost/overlaid-9734-web:qa web` passed after image-name qualification. Runtime image ID begins `6c5be3a3caf7`. Dependencies were installed from the frozen lockfile in the builder. |
| Named-volume repair | A fixture marker was created as `0:0` in an owned named volume. Normal API startup changed it to `100:101`; `/proc/1/status` showed the API running as UID 100. |
| Fresh product database | The packaged API used a new disposable database and reported healthy Postgres, bot runtime, retained state, history, dashboard config, sessions, and interaction storage. |
| Retention/restart | A BRB event was persisted through the API. Its complete SSE replay matched after a container restart, and its event ID remained in Postgres history. |
| Nginx routes/assets | Health, Sessions, and the demo alert route served the app shell. Both referenced assets loaded; responses carried `nosniff` and `SAMEORIGIN` headers. |
| Browser rendering | The collaborative browser loaded packaged Health and displayed the recovery console, healthy product storage checks, and build commit `2a0a295`. |
| Verification | `make verify`, all 21 `make smoke-browser` workflows, and `make smoke API_PORT=8083 WEB_PORT=5183` passed. |
| Cleanup | Both packaging containers, their owned volume, and their disposable database were removed. The collaborative browser returned to the native development API/web. Local image tags remain available for subsequent checks. |

The isolated API used loopback host networking to the disposable Postgres instance; the web container bound loopback port 5183. This verifies the packaged images, runtime permissions, routes, and persistence. It does not verify Compose dependency wiring, Docker-engine deployment, host-gateway OBS networking, production exposure, or live Twitch/OBS integration. Build date was `unknown` because this local QA build supplied only the commit argument. Expected missing Twitch configuration and unreachable OBS remained degraded.

Transient comparison data is outside git under `/tmp/overlaid-9734-package-drill`.
